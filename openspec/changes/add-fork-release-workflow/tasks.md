# Tasks

> 本清单对应的实现已完成并部署到生产。最终发布为 `fork-v0.2.17-3` / `0.2.17-ds.3`。
> 未勾选项是需要额外构造故障场景才能覆盖的演练，不影响当前发布与回退能力。

## 1. 版本元数据与探测

- [x] 1.1 在 `backend/cmd/server` 增加 `UpstreamVersion` 编译期变量，并由 `BuildInfo` 传到 `UpdateService`。
- [x] 1.2 保持 `BuildType` 编译期注入；新增 `fork` 类型，Docker release build 仍默认为 `release`。
- [x] 1.3 `deploy/Dockerfile` 增加 `ARG UPSTREAM_VERSION`、`ARG BUILD_TYPE=release`，写入 ldflags 和 OCI labels。
- [x] 1.4 扩展 `UpdateInfo`：`upstream_version`、`update_mode`、`can_self_update`。
- [x] 1.5 `has_update` 改为只比较 `upstream_version` 与官方 `latest_version`；不比较 fork release version。
- [x] 1.6 Redis 只缓存远端 Release，命中缓存时仍按当前进程的上游基线重新计算 `has_update`。
- [x] 1.7 对 fork 缺失/非法 `upstream_version` 返回可诊断状态，禁止静默显示绿色已最新。
- [x] 1.8 单元测试覆盖：已对齐、有新 Release、官方回退、预发布、缓存、非法元数据和 fork build。

## 2. 禁用官方在线变更

- [x] 2.1 fork build 下 `PerformUpdate`、`Rollback`、`RollbackToVersion` 返回 `UPDATE_MANAGED_EXTERNALLY`（409）。
- [x] 2.2 在线回退列表对 fork 视为不可用，不读取或展示官方回退版本。
- [x] 2.3 增加 handler 和 service 级测试，验证直接调用 API 也无法替换容器内二进制。
- [x] 2.4 保持官方 `release` / `source` build 的现有行为，减少后续上游合并冲突。

## 3. 左上角只读提醒

- [x] 3.1 新增 `ForkVersionPanel.vue`，在 `VersionBadge.vue` 增加独立 fork 分支。
- [x] 3.2 下拉中展示当前二开版本、已合入官方基线、官方最新 Release。
- [x] 3.3 有新版时保留琥珀色高亮、提示点、Release Notes 和外链。
- [x] 3.4 “立即更新”按钮改为 disabled，并显示“请通过二开版本发布流程更新”。
- [x] 3.5 fork build 不显示在线回退列表，改为只读的终端命令提示。
- [x] 3.6 检测失败和元数据缺失各自显示警告，不显示绿色勾。
- [x] 3.7 新部署后由镜像内 `upstream_version` 自动消警，不增加“已读”状态。
- [x] 3.8 增加中英文 i18n 和四种状态的组件测试。

## 4. 上游维护脚本

- [x] 4.1 新增 `scripts/fork-release.sh`，实现 `status`、`prepare`、`publish`。
- [x] 4.2 `status` fetch 上游并显示官方 Release、notes、落后的提交和文件、迁移差异。
- [x] 4.3 `prepare <tag>` 要求干净工作区，创建 `sync/<tag>` 分支并执行 `merge --no-ff`，不自动解冲突。
- [x] 4.4 `publish <tag>` 校验官方 tag 祖先关系、VERSION、分支和 tag 格式，计算下一个 `ds.N`。
- [x] 4.5 默认只创建本地 annotated tag；只有显式 `--push` 才推送。
- [x] 4.6 增加 shell 测试，确保输入错误、工作区脏或 tag 冲突时不产生提交、tag 或远端写入。

## 5. CI 与发布产物

- [x] 5.1 新增 `.github/workflows/fork-release.yml`，只接受 `fork-v*` tag 和手动 dry-run。
- [x] 5.2 校验 `fork-v<official>-<seq>`、`<official>-ds.<seq>`、官方 tag commit 和 source commit 一致。
- [x] 5.3 运行现有后端、前端、类型检查、生产构建和迁移检查。
- [x] 5.4 构建 `linux/amd64` 镜像，注入 `VERSION`、`UPSTREAM_VERSION`、`COMMIT`、`DATE`、`BUILD_TYPE=fork`。
- [x] 5.5 推送版本 tag 和 `sha-<short>` tag，不推送 `latest`，禁止覆盖已有 tag。
- [x] 5.6 生成 `release-manifest.json`、digest、SBOM、provenance，并创建 GitHub Release。
- [x] 5.7 manifest 记录 `max_migration`、`schema_epoch`、`database.rollback`；默认 `backup-required`，改成 `image-only` 必须有审查记录。
- [x] 5.8 完成一次真实 fork 镜像发布，产物通过 workflow 内版本和 OCI label 校验。

## 6. 服务器部署与回退

- [x] 6.1 新增 `deploy/docker-compose.fork.yml`，应用镜像只读取必填 `${SUB2API_IMAGE}`。
- [x] 6.2 新增服务器脚本 `sub2api-release`，支持 `status`、`deploy <image@digest>`、`rollback`。
- [x] 6.3 使用 `flock` 防并发；只重建 `sub2api`，不重建 PostgreSQL 和 Redis。
- [x] 6.4 部署前执行并验证 PostgreSQL `pg_dump -Fc`，记录备份路径和大小。
- [x] 6.5 原子更新 `.env`，记录 current/previous、版本、commit、digest、上游基线、backup 和健康状态。
- [x] 6.6 检查容器稳定运行、`/health` 和 `/app/sub2api --version`；失败按 manifest 策略恢复或停止。
- [x] 6.7 实现 previous 回退和显式 release+digest 回退；数据库不兼容时拒绝只切镜像。
- [x] 6.8 保留至少 3 个发布记录、3 个镜像和 3 份数据库备份，记录清理结果。
- [x] 6.9 更新 runbook，覆盖成功发布、健康失败、镜像回退、数据库恢复和故障定位。

## 7. 迁移命名

- [x] 7.1 确认各环境 `schema_migrations` 尚未记录 fork 的 `246/247` 文件名。
- [x] 7.2 将未上线的 fork 迁移改为 `dsapi_001_*`、`dsapi_002_*`，同步更新测试路径和迁移演练记录。
- [x] 7.3 确认所有环境均未执行旧文件名，因此无需 `schema_migrations` 兼容脚本；禁止对已执行环境直接改名。
- [x] 7.4 CI 检查二开迁移只使用 `dsapi_` 前缀，且顺序号不重复。

## 8. 验证与首次切换

- [x] 8.1 在数据库副本验证 `dsapi_001`、`dsapi_002` 之后的 schema 对上一镜像的兼容性。
- [x] 8.2 在受控生产窗口完成一次成功发布和一次显式回退，期间真实网关请求保持成功。
- [ ] 8.3 在非生产环境模拟健康检查失败，验证 `image-only` 自动恢复。
- [ ] 8.4 在非生产环境验证 `backup-required` 时脚本不会静默回退数据库。
- [x] 8.5 首次生产切换前记录当前官方 digest `sha256:44b59fc7...b1bb88` 作为 previous。
- [x] 8.6 生产部署首个二开 digest，确认版本、公开设置、管理员版本 API 和网关请求均正常。
- [ ] 8.7 模拟官方新版后确认徽标高亮、更新按钮禁用、Release Notes 可访问。
- [x] 8.8 部署包含该官方基线的二开版本后，确认徽标无需人工已读即可消警。

## 9. 生产结果

- [x] 9.1 最终发布 `fork-v0.2.17-3` / `0.2.17-ds.3`，digest 为 `sha256:fa16f102f5596bb515a426342ba068ace8414abca98a4fe4003d82d17e31625a`。
- [x] 9.2 服务器已完成 fork 部署、官方镜像回退、再次部署 fork 镜像。
- [x] 9.3 公共版本、CNY/shadow 配置、管理员版本接口和在线写接口 409 均验证通过。
- [x] 9.4 迁移后用户余额积分差异为 0，在线请求产生的 usage_logs 积分快照差异为 0。
- [x] 9.5 详细证据见 `verification.md`。
