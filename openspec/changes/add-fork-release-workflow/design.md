# 二开版本探测、发布与回退方案

## 结论

采用“官方 Release 只读探测，二开版本走 CI 镜像发布”的方式：

```text
GitHub 官方 Release
       │
       └── 只读探测 ──> 左上角徽标 / Release Notes / CLI status
                               │
                        人工判断并合并
                               │
fork-v0.2.18-1 ── GitHub Actions ──> GHCR 不可变镜像 + digest + manifest
                               │
                        服务器显式部署
                               │
                  备份 -> 切换 -> 健康检查 -> 记录状态
                               │
                  失败自动恢复 / 显式 rollback
```

三个版本概念必须分开：

```text
release_version          当前二开镜像版本，例如 0.2.18-ds.1
upstream_version         本次镜像已经合入的官方基线，例如 0.2.18
latest_official_version  GitHub 官方当前最新正式 Release，例如 0.2.19
```

左上角只比较后两个：

```text
latest_official_version > upstream_version  -> 显示官方有新版
latest_official_version <= upstream_version -> 清除高亮
```

不再用 `release_version` 和官方版本直接比较。是否允许应用内更新由编译期 `build_type` 决定，和是否有新版是两个独立问题。

## 1. 官方现有方案

官方当前流程已经核对，分为四层：

1. 管理端左上角 `VersionBadge.vue` 调用 `GET /api/v1/admin/system/check-updates`。
2. 后端固定查询 `Wei-Shaw/sub2api/releases/latest`，Redis 缓存 Release 数据 20 分钟，并比较当前程序版本和官方最新版本。
3. Release 构建点击“立即更新”后，`POST /api/v1/admin/system/update` 下载当前平台的官方归档，校验 SHA256，将当前可执行文件改名为 `.backup`，再原子替换为新二进制。
4. 更新完成后调用 `POST /api/v1/admin/system/restart`。回退可以恢复本地 `.backup`，也可以从最近 3 个更旧、非预发布的官方 Release 中选一个重新下载安装。

官方前端还有第三种状态：`build_type=source` 的源码构建只显示 `git pull` 提示，不提供在线更新。Docker 官方镜像以 `release` 构建，因此在页面上可以执行自更新。

这套流程不能直接用于本 fork：

- 下载的是官方二进制，不包含人民币、积分或本地二开逻辑。
- 容器内替换 `/app/sub2api` 只修改容器可写层，容器重建后会被镜像覆盖。
- `.backup` 和最近 3 个版本都来自官方发布序列，无法回退到 fork 自己的镜像。
- 官方回退只切二进制，不理解和撤销数据库迁移。
- 容器当前使用 `weishaw/sub2api:latest`，无法从 Compose 文件直接确认实际运行的版本。

结论是保留官方探测，封掉官方写操作，发布和回退改到镜像层完成。

## 2. 版本身份与构建元数据

每个二开发布固定为：

```text
官方基线：v0.2.18
应用版本：0.2.18-ds.1
Git tag：fork-v0.2.18-1
镜像 tag：ghcr.io/baisuipingan/sub2api:0.2.18-ds.1
生产引用：ghcr.io/baisuipingan/sub2api@sha256:<digest>
```

规则：

- `v0.2.18` 必须真实存在，并且是发布 commit 的祖先。
- 同一官方基线再次发布时递增为 `0.2.18-ds.2`、`0.2.18-ds.3`。
- 合并新的官方版本后，序号从 `ds.1` 重新开始。
- tag、应用版本和上游基线必须能相互推导，CI 不接受手工拼出来的不一致组合。
- `backend/cmd/server/VERSION` 保持官方版本号，减少上游合并冲突。
- 完整 `release_version` 和 `upstream_version` 通过 Docker build args 注入，不通过运行时环境变量伪造。
- fork Git tag 使用 `fork-v*`，不能使用 `v*-ds.*`。官方 workflow 监听 `v*`，错误的 tag 会意外触发官方二进制与镜像发布链。

### 数据库迁移与回退策略

仓库根目录的 `.fork-release.json` 是当前二开发布的数据库策略声明，CI 会读取并写入镜像 labels：

```json
{
  "schema_epoch": 1,
  "database": {
    "rollback": "image-only",
    "reason": "仅新增兼容表、列与触发器"
  }
}
```

本版本的两个迁移只增加积分列、表和触发器，现有官方应用仍可读写原字段，因此首次发布保持 `schema_epoch=1`、`rollback=image-only`。以后出现破坏性迁移必须递增 epoch 并改成 `backup-required`。

官方 migration runner 按文件名做字典序执行，以文件名为唯一键。二开继续占用 `246_...`、`247_...` 会和未来官方迁移编号冲突：文件不会重名，但新官方迁移晚到时会被插到已有的 fork 迁移之前，破坏原本的顺序语义。

在首次正式发布前，把尚未进入生产的 fork 迁移改成独立前缀：

```text
dsapi_001_billing_currency_credits.sql
dsapi_002_add_credit_quota_columns.sql
```

数字前缀属于官方，`dsapi_` 属于 fork，字典序也保证所有官方数字迁移先执行、fork 迁移后执行。生产库 2026-10-19 只应用到官方 `245`，尚未执行当前 fork 的 `246/247`，因此现在更名不会留下已应用的旧文件名记录。若某环境已经执行过旧文件名，不能直接改名；必须先记录并迁移 `schema_migrations`，否则会重复执行。

编译期元数据建议为：

```text
main.Version          0.2.18-ds.1
main.UpstreamVersion  0.2.18
main.Commit           <完整 source commit>
main.Date             <UTC 构建时间>
main.BuildType        fork
```

`build_type` 必须是镜像内不可变元数据。生产管理员不能在后台把它切成 `release` 来重新开放官方自更新。

### API 契约

在现有字段上向后兼容扩展：

```json
{
  "current_version": "0.2.18-ds.1",
  "upstream_version": "0.2.18",
  "latest_version": "0.2.19",
  "has_update": true,
  "build_type": "fork",
  "update_mode": "external",
  "can_self_update": false,
  "release_info": {
    "name": "Sub2API 0.2.19",
    "body": "...",
    "published_at": "2026-10-19T00:00:00Z",
    "html_url": "https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.19"
  },
  "cached": false
}
```

字段定义：

- `current_version`：二开发行版本，保持旧前端兼容。
- `upstream_version`：镜像已经合入的官方基线。
- `latest_version`：官方最新正式 Release。
- `has_update`：只表示 `upstream_version < latest_version`。
- `update_mode`：`self`、`external` 或 `manual`，由 build type 推导。
- `can_self_update`：只有官方 `release` 构建为 true；fork 永远为 false。
- Redis 只缓存 GitHub Release 数据，不缓存与本机版本相关的高亮结果。

如果 fork 镜像缺少有效的 `upstream_version`，界面显示“构建元数据缺失”，不把未知状态误报成已是最新。

## 3. 应用内更新与回退的硬禁用

新增 `build_type=fork`：

- `CheckUpdate` 继续请求官方仓库，所以左上角仍能探测新版和显示 Release Notes。
- `PerformUpdate`、`Rollback`、`RollbackToVersion` 在 fork 构建中返回明确错误，例如 `UPDATE_MANAGED_EXTERNALLY`，HTTP 使用 409 Conflict。
- `/system/rollback-versions` 对 fork 可以返回空的 `versions` 或相同禁用错误，前端不展示线上回退列表。
- 只禁前端按钮不够；直接调用 API 也不能替换容器里的二进制。

官方 `release` 和 `source` 行为保持兼容，避免未来合并上游代码时产生不必要冲突。

## 4. 左上角交互

保留现有版本徽标、刷新按钮、官方 Release 链接和 Release Notes，替换 fork 构建的分支：

| 状态 | 徽标 | 下拉内容 |
| --- | --- | --- |
| 官方基线已追平 | 灰色，绿色勾 | 当前二开版本、官方基线、官方最新版、Release 链接 |
| 官方有新版 | 琥珀色，带提示点 | 官方新版卡片、Release Notes、禁用的更新按钮、发布流程提示 |
| 检测失败 | 灰色，警告状态 | 错误原因、重试按钮；不显示误导性的绿色勾 |
| 元数据缺失 | 琥珀色警告 | 提示重建镜像，不提供更新操作 |

有官方新版时建议显示：

```text
当前二开版本  v0.2.18-ds.1
已合入官方    v0.2.18
官方最新      v0.2.19

[立即更新]  已禁用：请通过二开版本发布流程
[查看官方 Release]

终端操作：
./scripts/fork-release.sh status
```

不要显示在线回退列表，也不在 Web 进程里执行 Git、Docker 或 SSH。管理员可以通过页面看到“需要跟进”，具体合并、测试、构建和部署在终端完成。

新镜像部署后，`upstream_version` 已经变成 `0.2.19`，下一次页面加载或刷新会返回 `has_update=false`，不需要手工点击“已读”。这是版本状态，不使用前端本地已读标记。

建议把 fork 专用内容放进 `ForkVersionPanel.vue`，`VersionBadge.vue` 只增加一个分支，减少后续合并官方前端时的冲突面积。

## 5. 官方更新审阅与合并

本地脚本 `scripts/fork-release.sh` 提供三个维护阶段：

```bash
./scripts/fork-release.sh status
./scripts/fork-release.sh prepare v0.2.19
./scripts/fork-release.sh publish v0.2.19
```

### `status`

只读执行：

1. `git fetch upstream --tags --prune`。
2. 查询官方最新正式 Release 和 Release Notes。
3. 显示本地分支、当前官方基线、落后提交数、变更文件和数据库迁移差异。
4. 打印 GitHub compare/release 链接。

### `prepare <official-tag>`

要求工作区干净，然后：

1. 校验 tag 存在且来自 `Wei-Shaw/sub2api`。
2. 从当前集成分支创建 `sync/v0.2.19` 维护分支。
3. 执行 `git merge --no-ff v0.2.19`。
4. 单独列出新增迁移和定价、计费相关文件，供人工审查。

脚本不自动解决冲突，也不自动发布。冲突、迁移兼容性和业务语义必须人工确认。

### `publish <official-tag>`

在合并结果通过审查和测试后：

1. 校验 `v0.2.19` 是 HEAD 的祖先。
2. 检查 `backend/cmd/server/VERSION` 与官方基线一致。
3. 从已有 `fork-v0.2.19-*` tag 计算下一个 `ds.N`。
4. 创建 annotated tag `fork-v0.2.19-N`。
5. 由显式 `--push` 决定是否推送。默认只创建本地 tag，便于再次检查。

发布 tag 后 GitHub Actions 负责测试和镜像构建，本地不再保存生产构建产物。

## 6. CI 发布流水线

新增独立 `.github/workflows/fork-release.yml`，只接受 `fork-v*` tag 或手动 dry-run。不要修改官方 `release.yml` 的触发语义。

流水线阶段：

1. 校验 tag 格式 `fork-v<major>.<minor>.<patch>-<positive-int>`。
2. 校验 tag 指向当前 checkout，官方 `v<suffix>` 必须是祖先。
3. 校验 `VERSION`、官方 tag、release version 和 source commit 一致。
4. 检查是否有未预期的子模块或生成文件差异。
5. 运行后端、前端、迁移和生产构建检查。
6. 使用 `deploy/Dockerfile` 构建 `linux/amd64` 镜像。当前目标服务器为 x86_64；将来有 ARM 节点再增加 `arm64`。
7. 推送 `0.2.19-ds.1` 和 `sha-<短提交>` 两个 tag，记录实际 digest。
8. 生成 release manifest、SBOM 和 provenance。
9. 创建 GitHub Release，正文链接官方 Release Notes 并列出二开改动。
10. 禁用 tag 覆盖和 `latest` 发布。

主要 OCI labels：

```text
org.opencontainers.image.version=0.2.19-ds.1
org.opencontainers.image.revision=<source-commit>
org.opencontainers.image.created=<UTC timestamp>
io.baisuipingan.upstream.version=0.2.19
io.baisuipingan.upstream.commit=<official-tag-commit>
io.baisuipingan.release.channel=fork
io.baisuipingan.db.rollback=image-only|backup-required
```

`deploy/Dockerfile` 当前把 `main.BuildType=release` 写死。实施时需要增加 `ARG BUILD_TYPE=release`、`ARG UPSTREAM_VERSION`，并由 fork workflow 强制传入 `fork` 和对应官方版本。

## 7. Release manifest

每次发布生成并保存如下清单，文件示例见 `release-manifest.example.json`：

```json
{
  "schema_version": 1,
  "release_version": "0.2.19-ds.1",
  "upstream_version": "0.2.19",
  "upstream_commit": "<tag commit>",
  "source_commit": "<build commit>",
  "git_tag": "fork-v0.2.19-1",
  "image": "ghcr.io/baisuipingan/sub2api:0.2.19-ds.1",
  "digest": "sha256:<digest>",
  "built_at": "2026-10-19T00:00:00Z",
  "database": {
    "max_migration": "dsapi_002_add_credit_quota_columns.sql",
    "schema_epoch": 1,
    "rollback": "image-only"
  },
  "tests": {
    "backend": "passed",
    "frontend": "passed",
    "image_smoke": "passed"
  }
}
```

`schema_epoch` 表示数据库兼容代际。只新增表、列、索引和可空字段时保持同一个 epoch；删除、重命名、收紧约束或修改既有语义时递增 epoch。

`database.rollback` 只有两个值：

- `image-only`：本版本增加或使用的数据库变更对上一应用版本向后兼容，允许只切旧镜像。
- `backup-required`：迁移包含删除、重命名、收紧约束或业务不可逆变化，回退必须恢复部署前数据库备份。

默认值必须是 `backup-required`。只有审阅迁移并验证“上一镜像 + 新 schema”后才能改成 `image-only`。部署脚本同时比较 manifest 的 `schema_epoch`：epoch 不同则不允许自动回退镜像，即使操作员传了旧 digest。

## 8. 生产部署

服务器继续使用 `/root/sub2api-deploy`。新增只覆盖应用镜像的 Compose overlay：

```yaml
services:
  sub2api:
    image: ${SUB2API_IMAGE:?SUB2API_IMAGE is required}
```

所有 Compose 操作固定使用：

```bash
docker compose \
  -f docker-compose.yml \
  -f docker-compose.fork.yml \
  --env-file .env \
  <action>
```

部署命令：

```bash
/root/sub2api-deploy/bin/sub2api-release deploy \
  ghcr.io/baisuipingan/sub2api@sha256:<digest>
```

执行顺序：

1. 获取 `flock`，防止并发发布。
2. 读取当前容器镜像和 release manifest。
3. 校验目标引用必须是 digest，拒绝 `latest` 和裸版本 tag。
4. 检查当前容器健康、磁盘空间、Compose 配置和 GHCR 拉取权限。
5. 使用 PostgreSQL `pg_dump -Fc` 生成部署前备份，并校验 dump 非空、可被 `pg_restore --list` 读取。
6. `docker pull <digest>`，记录实际 image ID。
7. 原子更新 `.env` 中的 `SUB2API_IMAGE`，保留原文件的备份。
8. 只重建 `sub2api` 容器，不重建 PostgreSQL 和 Redis。
9. 等待 `/health`、容器持续运行和二进制版本号检查通过。
10. 将 current/previous 指针、digest、backup 路径、上游基线和检查结果写入 `releases/state.json` 及单独的发布记录。
11. 清理超过保留期的旧镜像和备份，只保留最近至少 3 个发布记录和 3 份备份。

第一版仍是单容器短时重建，不宣称零停机。它提供的是自动变更有记录、健康失败可恢复和显式版本追踪。若以后要求零停机，需要另做双实例和代理切换，而不是把复杂度塞进本次脚本。

## 9. 回退与数据库边界

上一版本回退：

```bash
/root/sub2api-deploy/bin/sub2api-release rollback
```

脚本读取 previous digest，重复部署流程，但不重复创建 current/previous 之外的隐式状态。

更早版本：

```bash
/root/sub2api-deploy/bin/sub2api-release rollback \
  --release 0.2.17-ds.1 \
  --digest sha256:<digest>
```

数据库规则：

- 部署前总是备份，镜像回退不等于数据库回退。
- 目标 manifest 为 `image-only` 且 `schema_epoch` 相同，允许只切镜像，保留发布后的新业务数据。
- 目标 manifest 为 `backup-required`、`schema_epoch` 不同或数据库已执行更高版本迁移时，默认拒绝回退并打印所需备份路径。
- 数据库恢复必须由管理员显式确认，因为它会丢失备份时间点之后的数据，不能静默自动执行。
- 新版本健康检查失败时，如果没有执行不兼容迁移，自动恢复 previous 镜像；如果已经进入不可逆 schema，停止自动恢复并要求人工处理，避免把“应用失败”扩大成“数据回退”。

迁移采用 expand/contract：

1. 新版本先增加新列、表和索引，旧版本仍可运行。
2. 两个发布周期后再删除旧列或收紧约束。
3. 回退安全只承诺一个发布周期，不做无限历史回退。
4. 任何 destructive migration 都必须在发布说明中明确标注 `backup-required`。

## 10. 当前服务器基线

2026-10-19 只读检查结果：

- 容器：`sub2api`，状态 healthy。
- 镜像：`weishaw/sub2api:latest`。
- 实际程序：`0.2.17`，commit `060d0160417782cd962a0c7f460f6cd4565d4916`。
- 运行镜像 digest：`sha256:44b59fc74b0b582750ef5f38146782db177767ef9d3f34351f1c75c157b1bb88`。
- 应用端口：`127.0.0.1:8080`，未绑定公网地址。
- 平台：`linux/amd64`。
- Compose：v5.6.0，当前目录只有官方生成的 `docker-compose.yml`。

首次切换时，上述官方 digest 可作为初始 `previous` 记录。不要把 `latest` 继续作为生产部署的可变指针。

## 11. 实施顺序

1. 先实现 `fork` build type、`upstream_version` 和写接口 hard guard。
2. 改造左上角为只通知模式，补 API 与前端状态测试。
3. 增加 fork tag 校验、发布脚本和不触发官方 workflow 的独立 CI。
4. 生成 manifest 和不可变 GHCR 镜像，先完成一次不部署的 dry-run。
5. 增加 Compose overlay、部署/回退脚本和状态目录。
6. 在数据库副本与 staging 完成成功发布、健康失败自动恢复、显式回退演练。
7. 首次生产切换固定首个二开 digest，并保留上述官方镜像 digest 作为 previous。
8. 以后按“探测 -> 审阅 -> 合并 -> 测试 -> tag -> CI -> deploy”循环。

## 风险与取舍

- 官方新版提醒只覆盖 GitHub Release，不覆盖尚未发 Release 的普通 commit；这与官方当前行为一致。
- 同一官方基线上的 fork 补丁不会触发官方新版高亮，依赖 fork Release 记录追踪，不伪造上游更新状态。
- 单实例重建有短暂停机；换来的是简单、清晰、可自动恢复的状态机。
- 固定 digest 会比 `latest` 多一步发布操作，但保证线上内容可复现和可审计。
- 数据库能否只回退镜像必须靠发布时明确标注和 staging 验证，不能由版本号自动推断。
