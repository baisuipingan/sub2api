# 实施与验证记录

## 本地检查

- 后端：`go test -tags unit ./internal/server ./internal/service ./internal/handler/admin ./cmd/server` 通过。
- 后端集成：repository、middleware、routes、service 等完整 integration suite 通过。
- 主分支 CI：https://github.com/baisuipingan/sub2api/actions/runs/39242637781 通过单元、集成、lint、前端和 shell 检查。
- 前端：373 个测试文件、2875 个测试通过；TypeScript 检查通过；Vite production build 通过。
- Shell：`scripts/fork-release.sh`、`deploy/sub2api-release.sh` 语法检查通过，fork release shell 测试通过。
- 工作流 YAML 可正常解析。

## 生产备份迁移演练

2026-10-19 从生产服务器生成并下载了新的 PostgreSQL custom dump：

```text
source: pre-fork-20261019T104931Z.dump.gz
sha256: 1b50d5577afa75c0f352baa58a6db38008a53aff403f3b17d2e634f0efcf245d
production schema: 245_gemini_cached_contents_mutation_lease.sql
```

已将备份恢复到本机一次性 `postgres:18-alpine`，“`sub2api`”数据库中：

- 依次执行 `dsapi_001_billing_currency_credits.sql` 和 `dsapi_002_add_credit_quota_columns.sql` 成功。
- 用户余额与积分差异行数：0。
- 2854 条 usage_logs 的 `charge_credits` 与 `actual_cost × 100000000` 差异行数：0。
- `billing_ledger` 表成功创建。
- 用户积分同步触发器存在。
- 演练容器和本机临时备份副本已清理。

本次数据只用于结构验证，记录中没有输出邮箱、密钥或余额明细。

## 首次上线基线

切换前生产运行：

```text
image: weishaw/sub2api:latest
program: 0.2.17
digest: sha256:44b59fc74b0b582750ef5f38146782db177767ef9d3f34351f1c75c157b1bb88
health: ok
schema: 245_gemini_cached_contents_mutation_lease.sql
```

部署前独立备份：

```text
/root/sub2api-deploy/releases/backups/pre-fork-20261019T104931Z.dump.gz
758 KiB, gzip integrity verified
```

## fork 镜像与回退验证

最终发布：

```text
Git tag: fork-v0.2.17-3
release: 0.2.17-ds.3
source commit: 4f3e40810eb7ace9877423df37b6de1afa67e3f4
upstream: v0.2.17
digest: sha256:fa16f102f5596bb515a426342ba068ace8414abca98a4fe4003d82d17e31625a
Fork Release run: https://github.com/baisuipingan/sub2api/actions/runs/39247264224
```

`ds.1` 在修正旧 API 契约前失败，`ds.2` 的发布运行被 GitHub 侧取消；两个 tag 均未用于生产，也未复用。最终只部署 `ds.3`。

发布工作流通过：

- 后端单元测试。
- 后端数据库集成测试。
- golangci-lint。
- 前端完整 Vitest、TypeScript、Vite build。
- release shell tests，包括 rollback metadata 测试。
- fork tag、官方祖先、VERSION 和 dsapi migration namespace 校验。
- linux/amd64 镜像构建、镜像内 OCI labels 和二进制版本校验。
- GitHub Release 与 release manifest 生成。

服务器发布：

1. 从官方 `weishaw/sub2api@sha256:44b59fc7...b1bb88` 切换到 fork digest。
2. 部署前数据库备份：`sub2api-20261019T121200Z.dump.gz`。
3. `dsapi_001`、`dsapi_002` 成功记入 `schema_migrations`。
4. 公共设置返回 `version=0.2.17-ds.3`、`billing_engine=shadow`、`billing_display_currency=CNY`、`billing_credits_per_usd=100000000`。
5. 管理员版本 API 返回：`build_type=fork`、`upstream_version=0.2.17`、`latest_version=0.2.17`、`has_update=false`、`update_mode=external`、`can_self_update=false`。
6. 直接调用 update 和 rollback API 均返回 `409 UPDATE_MANAGED_EXTERNALLY`。
7. 应用端口仍只监听 `127.0.0.1:8080`，公网直接访问 8080 超时。

真实回退演练：

- 执行 `sub2api-release rollback`，切回官方 digest，容器 healthy，公开版本恢复为 `0.2.17`。
- 回退期间实际网关请求继续返回 200，证明新增 schema 对上一官方版本兼容。
- 回退前备份：`sub2api-20261019T121307Z.dump.gz`。
- 之后重新部署 fork digest，备份：`sub2api-20261019T121341Z.dump.gz`，最终容器 healthy。

数据对账：

- 用户余额与 `balance_credits` 差异：0。
- 历史及新请求的 `charge_credits` 与 `actual_cost × 100000000` 差异：0。
- 回退窗口内由官方版本写下的 2 条记录 `charge_credits=0`，已在确认唯一 ID 后按同一换算公式补齐；最终 fork 版本的新请求均直接写入正确积分。
- `billing_ledger` 可正常创建和使用。

服务器基础 Compose 的应用 `image` 已改为 `${SUB2API_IMAGE:?SUB2API_IMAGE is required}`，原文件保存为 `docker-compose.yml.before-fork`。即使直接执行 `docker compose up -d`，也会继续使用 `.env` 中的固定 digest，不会退回 `latest`。

当前发布记录保存在服务器：

```text
/root/sub2api-deploy/releases/current.json
/root/sub2api-deploy/releases/previous.json
/root/sub2api-deploy/releases/history.jsonl
/root/sub2api-deploy/releases/records/0.2.17-ds.3.json
```

当前 `previous` 是官方 0.2.17 digest，可用于同 schema epoch 的快速镜像回退。后续若发布 `backup-required` 迁移，脚本会拒绝不安全回退并要求恢复数据库备份。
