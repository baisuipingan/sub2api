# Implementation Evidence — 第一阶段

## 官方代码同步

- Fork `origin/main`：`b0a02a330`
- 官方 `Wei-Shaw/sub2api/main`：`b0a02a330`
- 同步结果：无分叉、无需合并。
- 工作分支：`feat/cny-credit-billing`

## 已实现

### 账本基础

- 新增版本化 `billing_currency_config`，支持 `legacy/shadow/credits` 引擎和 USD/CNY 展示。
- 迁移后默认启用 CNY 展示和 `shadow` 引擎，待对账后再人工切换到 `credits`。
- 固定 `1 USD = 100,000,000 credits`，使用 Decimal 做单次 half-away-from-zero 换算。
- 展示汇率和账本刻度独立；人民币充值售价已保存在配置中，但当前仍由现有支付倍率控制。
- 进程内使用原子快照，过期后异步刷新；另一实例更新配置后不阻塞请求热路径。

### 数据库

- `users.balance_credits`、`frozen_balance_credits` 与旧 USD 兼容列双向同步触发器。
- API Key 配额、限速窗口和已用量增加整数 credits 列，计费引擎为 `credits` 时直接更新积分列。
- 订阅日/周/月用量增加 credits 列并在计费事务中直接更新。
- 分组限额、平台限额和兑换码面额增加 credits 镜像，由触发器与旧字段双向同步。
- `usage_logs.charge_credits`、`billing_currency`、`credits_per_usd`、`usd_to_cny_rate`、`billing_version`。
- 余额订单创建时即写入 `credited_credits`、`bonus_credits`、充值汇率和配置版本快照。
- 不可变 `billing_ledger` 表及请求幂等唯一索引。
- 历史余额、用量和充值订单在迁移中回填积分快照。

### 扣费

- 统一 billing command 增加积分和配置快照。
- `credits` 引擎下，钱包扣费改为原子更新 `balance_credits`，旧余额由触发器同步。
- 充值履约成功时在同一兑换码事务内写 `payment` ledger；订单回调和履约重试仍由订单租约与唯一索引保护。
- 同一 PostgreSQL 事务内写入 `billing_ledger`。
- Shadow 引擎只计算 `charge_credits`，继续使用旧扣费路径。
- 请求指纹不包含展示汇率或配置版本，避免切换展示配置后重试被误判冲突。

### 管理与前端

- 新增 `GET/PUT /api/v1/admin/settings/billing-currency`。
- 公开展示配置增加币种、汇率、积分刻度和计费引擎。
- 支付设置页新增人民币计费卡片。
- 新增统一 `formatCurrency` / `formatCredits` / `formatBillingAmount`。
- 已覆盖余额、顶栏、仪表盘、充值页、订阅、Key 用量、兑换、订单、批量图片和模型价格展示。
- 管理端新增计费币种、展示汇率和引擎切换；充值售价输入暂不展示，避免让管理员误以为它已替代现有充值倍率。
- Key quota 输入按当前展示币种展示，提交时换算回后端兼容的 USD 等值。
- 余额不足和充值成功邮件的用户金额改用服务端展示币种格式化；上游账号成本提醒仍保持 USD。

## 仍需后续阶段

- 平台限额写路径同时更新 USD 等值列与 credits；预检和缓存仍读取 USD 等值投影以保持兼容。
- 用户端和外部 API 仍主要返回 USD 等值兼容字段，credits 字段的公开响应兼容层尚未完成。
- 充值履约在兑换码事务内直接增加 `balance_credits` 并写入 `payment` ledger；退款和返利尚未全部写入统一 ledger。
- 尚未实现生产库 shadow 对账任务和自动余额差额告警。
- 批量图片冻结逻辑仍使用旧 `frozen_balance` 兼容列，触发器同步 `frozen_balance_credits`。
- 管理端部分内部成本视图继续显示 USD，作为上游成本分析口径。

## 验证

- `go test -count=1 ./...`：通过。
- `go test -tags=unit -count=1 ./internal/service ./internal/repository ./internal/handler/...`：通过。
- `node_modules/.bin/vue-tsc --noEmit`：通过。
- `node_modules/.bin/vitest run`：372 个测试文件、2871 个测试通过。
- 修改文件 ESLint：通过。
- `node_modules/.bin/vite build`：通过。
- 迁移静态校验：通过。

## 数据库演练

- 已使用高投产备份在 PostgreSQL 18.6 容器中完成 246/247 迁移演练，详情见 `rehearsal-report-2026-10-19.md`。
- 备份内实际用户和历史用量回填差异为 0；真实 Repository 扣费与幂等测试通过。

## 上线前必须完成

1. 在生产数据库副本执行迁移，核对用户购买力、历史余额、usage_logs 和订单积分快照。
2. 以 `shadow` 引擎部署并运行至少一个账期，对账 `users.balance_credits`、`usage_logs.charge_credits` 和 `billing_ledger`。
3. 确认所有实例都运行支持 credits 的版本后，再切换到 `credits`。
4. 备份数据库并保留回滚到 `legacy` 的配置开关。
