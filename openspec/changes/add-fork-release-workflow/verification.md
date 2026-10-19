# 实施与验证记录

## 本地检查

- 后端：`go test -tags unit ./internal/server ./internal/service ./internal/handler/admin ./cmd/server` 通过。
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

> 待真实 fork 镜像发布和服务器切换后补充。
