# 数据库迁移演练报告 — 2026-10-19

## 环境

- 备份格式：PostgreSQL 18.6 plain SQL dump，gzip 完整。
- 演练环境：一次性 `postgres:18-alpine` 容器，仅绑定 `127.0.0.1`，未使用生产连接。
- 恢复方式：完整恢复后直接执行 `246`、`247` 两个迁移。
- 演练数据不包含敏感信息输出；临时扣费使用测试用户和测试 Key，结束后已清理。
- 演练容器、临时卷和容器内备份副本均已删除。

## 备份基线

- 用户、API Key 和 usage_logs 均包含真实数据。
- payment_orders、user_subscriptions、user_platform_quotas 和 redeem_codes 为空。
- 出于生产数据保护，报告不记录邮箱、账户标识和具体金额。
- 精确汇总只用于本地演练比对，不写入仓库。

## 迁移结果

- 迁移前 `schema_migrations` 最新记录：`245_gemini_cached_contents_mutation_lease.sql`。
- `246_billing_currency_credits.sql` 执行成功（正式发布前已更名为 `dsapi_001_billing_currency_credits.sql`）。
- `247_add_credit_quota_columns.sql` 执行成功（正式发布前已更名为 `dsapi_002_add_credit_quota_columns.sql`）。
- `billing_ledger` 表和四个索引创建成功。
- 6 张表的积分双向同步触发器创建成功。
- 默认配置写入成功：`engine=shadow`、`display_currency=CNY`、`usd_to_cny=7.2`。

## 数据对账

- 用户余额总和在迁移前后保持一致。
- 所有用户 `balance_credits` 与 `balance × 100000000` 的回填差异为 0。
- usage_logs 行数保持不变，所有日志的 `charge_credits` 与逐行 `actual_cost × 100000000` 差异为 0。
- API Key、分组限额的逐行回填差异为 0。
- 历史用量按每条日志独立四舍五入，聚合后只存在极小的逐行取整差异，单行误差为 0。

## 触发器验证

在事务中分别执行旧字段更新和积分字段更新，随后回滚：

- 用户 `balance ↔ balance_credits`：通过。
- API Key `quota/quota_used/rate/usage ↔ *_credits`：通过。
- 分组日限额：通过。
- 订阅日/周/月用量：通过。
- 用户平台限额与用量：通过。
- 兑换码面额：通过。

## 真实扣费 Repository 验证

使用仓库中的 `UsageBillingRepository.Apply` 对恢复库执行两次相同 request_id 的积分扣费：

```text
first_applied=true
second_applied=false
balance=8.75
balance_credits=875000000
quota_used=1.25
quota_used_credits=125000000
usage_5h=1.25
usage_5h_credits=125000000
billing_ledger rows=1
delta_credits=-125000000
balance_after=875000000
```

结果符合预期：第二次请求没有重复扣费，余额、Key 配额、限速用量和流水完全一致。

## 演练中修复的问题

1. `billing_ledger` 使用 `-$2` 时 PostgreSQL 无法推断参数类型，改为显式 `bigint` 运算。
2. `jsonb_build_object` 的参数缺少类型转换，已显式转换为 `text/numeric/bigint`。
3. 充值履约写入积分快照时更新时间会破坏 fulfillment lease 的版本判断，已保留原 lease 时间。
4. 充值履约的积分余额更新与 ledger 写入已放入同一个兑换码事务。
5. PostgreSQL 不接受未转换的 text 直接写入 `jsonb`；充值 ledger 改为 Ent `BillingLedger` upsert，统一处理 JSON 编码和唯一冲突。

## 集成测试

```text
go test -tags=integration ./internal/repository -run 'TestUsageBillingRepositoryApply|TestMigrationsSchema'
go test -tags=integration ./internal/repository -run TestUsageBillingRepositoryApply_CreditsWritesLedger
```

结果：通过。该测试使用全新临时 PostgreSQL，由项目自己的迁移 runner 执行全部迁移。

## 结论

这份高投产备份可以安全进入 `shadow` 灰度。余额和历史用量均保持购买力，回填无差异，触发器和真实扣费路径通过。

正式切换 `credits` 前仍需在部署环境观察 shadow 对账，并补充包含支付订单、退款、订阅和返利数据的备份演练。
