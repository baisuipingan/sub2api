# 人民币计费与自由切换方案（积分账本）

## 结论

采用一条固定内部账本，而不是同时维护美元余额和人民币余额：

```text
内部账本：credits（BIGINT 整数积分）
展示币种：USD / CNY，仅影响 UI
计价基准：pricing_usd，保留 USD 模型价格
用户应付：charge_credits，由实际应付 USD 金额换算
```

这样可以做到：

- 全站默认显示人民币。
- 管理端随时切换 USD/CNY；内部 credits 和已发生扣费不变，页面只按新币种重新格式化。
- 模型价格目录继续使用上游 USD 成本，不需要重写全部定价数据。
- 请求扣费、订阅额度、API Key 配额和统计只有一种内部单位，避免双钱包和汇率对账。
- 多实例部署时以 PostgreSQL 为唯一账务真相，Redis 只做缓存和预扣。

这与 NewAPI 的核心思路一致：NewAPI 使用固定的内部 `quota`，展示层再根据 `USDExchangeRate` 显示 USD/CNY；充值售价由单独的 `Price` 控制。本方案保留这个分层，但使用整数积分、Decimal 转换和数据库幂等，避免浮点误差和全局变量问题。

## 1. 核心计费模型

### 1.1 固定积分刻度

建议固定：

```text
1 USD = 100,000,000 credits
```

即 `credits_per_usd = 100000000`，对应 8 位小数精度，和现有 `decimal(20,8)` 余额精度接近。该值上线后不可修改；如果未来必须修改，只能做一次带版本号的数据迁移。

```text
total_cost_usd = 现有 CostBreakdown.TotalCost（计价基础，尚未乘分组/用户倍率）
billable_usd   = 现有 CostBreakdown.ActualCost（应用倍率后的用户应付等价 USD）
charge_credits = round_half_up(billable_usd × credits_per_usd)
```

模型价格、渠道定价和供应商成本仍可以是 USD。USD 在这里只是“计价基准”，不是用户余额单位。现有 `AccountStatsCost` 继续用于上游成本和毛利分析。

### 1.2 展示换算

```text
USD 展示金额 = credits / credits_per_usd
CNY 展示金额 = credits / credits_per_usd × usd_to_cny
```

`usd_to_cny` 只用于展示，例如 `7.20`，不参与已经发生的扣费。

### 1.3 充值与售价

充值和展示必须分开配置，否则活动和渠道手续费会污染余额换算：

```text
usd_to_cny:              1 USD 的积分在页面显示为多少 CNY
recharge_rates["CNY"]:   用户支付多少 CNY 才能买到 1 USD 等值积分
recharge_rates["USD"]:   美元渠道售价倍率，通常为 1.0
```

任意支付币种到账：

```text
paid_amount = 用户实付金额
credited_usd_equivalent = paid_amount / recharge_rates[paid_currency]
credited_credits = round(credited_usd_equivalent × credits_per_usd)
```

`recharge_rates["CNY"]` 和展示用的 `usd_to_cny` 可以不同。前者决定充值的实际售价，后者只决定余额显示，这样能独立做折扣、渠道手续费或美元卡定价。

订单必须记录充值币种、实付金额、到账积分和当时的充值售价。以后改汇率只影响新订单。

第一阶段保留现有 `BalanceRechargeMultiplier` 作为实际充值售价，配置中的 `recharge_rates` 只作为下一阶段的显式售价模型；启用前不得在管理端把该字段描述成已生效。

示例：`credits_per_usd=100000000`，`usd_to_cny=7.2`，`recharge_rates["CNY"]=7.2`。

```text
旧系统 TotalCost = $0.20
分组倍率 1.2，旧系统 ActualCost = $0.24
charge_credits = 0.24 × 100000000 = 24000000 credits
CNY 展示 = 24000000 / 100000000 × 7.2 = ¥1.728
```

## 2. 配置设计

系统设置表新增一个版本化 JSON，或者拆成以下键：

```json
{
  "enabled": true,
  "engine": "shadow",
  "credits_per_usd": 100000000,
  "display_currency": "CNY",
  "usd_to_cny": 7.2,
  "recharge_rates": {
    "CNY": 7.2,
    "USD": 1.0
  },
  "rate_updated_at": "2026-10-19T00:00:00Z",
  "version": 1
}
```

`engine` 取值：

```text
legacy  — 继续按旧 USD 字段扣费
shadow  — 同时计算积分但不实际扣积分，用于对账
credits — 正式按积分扣费
```

约束：

- `credits_per_usd` 上线后不可改。
- `display_currency`、`usd_to_cny` 可随时改，只影响显示。
- `recharge_rates` 可随时改，只影响新订单；订单保存快照。
- 配置读取使用内存原子快照，数据库为源；更新后广播失效，失败时继续使用最后一次有效配置。
- 不在每次 API 请求中访问第三方汇率接口。

## 3. 数据模型

不要重命名或直接改写旧 USD 字段。新增积分字段，保留旧字段用于迁移和回滚。

第一阶段采用兼容双写：余额、API Key 配额、订阅用量、分组/平台限额和兑换码都同时保存 USD 等值列与 credits 列，数据库触发器负责双向同步；`credits` 引擎的扣费路径直接写 credits，并同步兼容列。这样现有缓存、预检和旧接口不需要一次性全部停用。

### 3.1 用户余额

`users` 新增：

```sql
balance_credits BIGINT NOT NULL DEFAULT 0
balance_credits_version BIGINT NOT NULL DEFAULT 0
```

`users.balance` 暂时保留为旧逻辑兼容字段。最终余额以 `balance_credits` 为准。

### 3.2 用量日志

`usage_logs` 新增：

```sql
charge_credits       BIGINT NOT NULL DEFAULT 0
input_credits        BIGINT NOT NULL DEFAULT 0
output_credits       BIGINT NOT NULL DEFAULT 0
cache_read_credits   BIGINT NOT NULL DEFAULT 0
cache_write_credits  BIGINT NOT NULL DEFAULT 0
quota_per_usd_snapshot NUMERIC(30,0)
usd_to_cny_snapshot    NUMERIC(20,8)
pricing_version        VARCHAR(64)
```

`total_cost`、`actual_cost` 继续保存 USD 等价成本，用于上游成本和毛利分析；`charge_credits` 是实际扣费真相。

### 3.3 账务流水

新增不可变流水表：

```sql
CREATE TABLE billing_ledger (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL,
  delta_credits BIGINT NOT NULL,
  balance_after BIGINT NOT NULL,
  bucket_type VARCHAR(32) NOT NULL DEFAULT 'wallet', -- wallet / subscription
  bucket_id BIGINT NOT NULL,
  reference_type VARCHAR(32) NOT NULL,
  reference_id VARCHAR(128) NOT NULL,
  request_id VARCHAR(128),
  metadata JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX billing_ledger_idempotency
  ON billing_ledger (bucket_type, bucket_id, reference_type, reference_id);
```

余额或订阅额度变化和流水写入必须在同一个 PostgreSQL 事务中完成。唯一索引是重试和异步任务幂等的最后一道保险。API Key 的 quota 是限制值，用量可继续由 `usage_logs` 汇总，不必额外记 ledger。

### 3.4 配额与限制

这些用户的“额度”都必须改为 credits：

- `api_keys.quota`
- `api_keys.quota_used`
- `api_keys.rate_limit_*`
- `api_keys.usage_*`
- `groups.daily_limit_usd` / `weekly_limit_usd` / `monthly_limit_usd`
- `user_subscriptions.daily_usage_usd` / `weekly_usage_usd` / `monthly_usage_usd`
- `user_platform_quotas.*`
- 新用户初始余额、余额通知阈值、返利上限、兑换码面额

建议新增对应 `*_credits BIGINT` 字段，旧字段只作为迁移来源。管理端输入当前展示币种，保存时换算为 credits，并把 `limit_currency`、`rate_snapshot` 一起保存。

### 3.5 订单

`payment_orders` 新增：

```sql
paid_currency       VARCHAR(3)
paid_amount         NUMERIC(20,8)
credited_credits    BIGINT
bonus_credits       BIGINT
credits_per_usd_snapshot NUMERIC(30,0)
usd_to_cny_snapshot      NUMERIC(20,8)
recharge_rate_snapshot   NUMERIC(20,8)
billing_version          VARCHAR(64)
```

订阅订单不一定发放钱包积分，但仍要保存支付币种和汇率快照。

`redeem_codes` 新增 `value_credits`，新的余额兑换码只写 credits；旧码迁移时按迁移时刻固定换算。

`subscription_plans.price` 和 `currency` 继续表示商品售价，必须明确 `currency` 是真实支付币种，不再只是展示标签。

## 4. 扣费链路

### 请求前

1. 按现有模型价格计算 USD 估算成本。
2. 用同一套转换函数估算 credits。
3. 检查 `balance_credits`、API Key quota、订阅额度和平台限额。
4. Redis 只登记在途预留，TTL 自动释放。

### 请求完成

1. 计算最终 USD 成本。
2. 转换为 `charge_credits`，只在这里做一次四舍五入。
3. 在一个事务中：
   - 条件扣减 `users.balance_credits`；
   - 更新 API Key 和订阅的 credits 用量；
   - 写 `usage_logs`；
   - 写 `billing_ledger`。
4. 事务成功后失效用户级缓存；失败则由异步重试，`request_id` 保证不会重复扣费。

核心 SQL 形态：

```sql
UPDATE users
SET balance_credits = balance_credits - $1,
    balance_credits_version = balance_credits_version + 1
WHERE id = $2 AND balance_credits >= $1
RETURNING balance_credits;
```

不要用“先读余额，再在应用层判断，再写回”的方式处理并发扣费。

## 5. 充值、退款和订阅

### 充值

- 支付网关的 `pay_amount` 保存真实法币金额。
- 到账时按订单快照换算成 credits，不按当前汇率重新计算。
- 赠金直接记录 `bonus_credits`，不要记录为 USD 金额。
- 订单完成、到账、退款都要写同一张 ledger。

### 退款

- 退款法币金额沿用原支付币种。
- 余额侧冲正使用订单的 `credited_credits`，不按退款当日汇率重新换算。
- 若用户已消费部分积分，退款策略需要明确：允许负余额，或只退未消费部分。两种策略都必须在服务层写死并测试。

### 订阅

- 订阅套餐价格是真实法币商品价格。
- 订阅用量和日/周/月限额使用 credits。
- 订阅扣费与钱包扣费共用积分转换函数，但写入不同 bucket；订阅钱包不使用 `users.balance_credits`。

### 兑换码和返利

- 余额兑换码直接增加 credits。
- 邀请返利按比例计算，返利上限也保存为 credits。
- 所有人工调整必须写 ledger，并记录管理员 ID 和原因。

## 6. 前端和 API

### 统一格式化

建立唯一的货币配置和后端格式化入口：

```text
backend:  ConvertUSDToCredits / ConvertCreditsToDisplay / ConvertPaidToCredits
frontend: formatCredits(credits, displayCurrency, rate)
```

前端禁止在页面中直接拼接 `$`、`¥` 或使用 `toFixed()` 充当金额格式化。现有支付组件已经有 `formatPaymentAmount`，可以保留；另外新增统一的账单/余额格式化函数，并替换所有硬编码符号。

API 返回：

```json
{
  "balance_credits": "1388888889",
  "display_currency": "CNY",
  "display_balance": "100.00"
}
```

后端计算权威金额，前端只负责展示。客户端传来的金额不得作为扣费依据。

### 展示规则

- 用户余额、订阅额度、Key 配额、用量统计：按当前 `display_currency` 展示；这是换算后的视图，不改变 credits。
- 模型官方成本：可以标注为“USD 成本”，用于管理员分析。
- 订单历史：显示订单真实的支付币种和金额，例如 `¥100.00`。
- 充值页：明确显示“实付 ¥100，到账 ¥100 等值额度”，避免把充值售价和展示汇率混在一起。

## 7. 迁移和灰度

### 第一步：只加字段

新增字段、配置和 ledger，不改变现有扣费行为。旧字段全部保留。

### 第二步：回填

在业务低峰执行一次性迁移：

```text
balance_credits = round(balance × credits_per_usd)
quota_credits   = round(旧 USD 配额 × credits_per_usd)
usage_credits   = round(旧实际扣费 × credits_per_usd)
```

这里的旧 `balance` 按当前系统语义是 USD 等价余额，不能当成 CNY 直接除以汇率。迁移应保持用户的购买力，不因为切换展示币种而改变余额实际价值。

历史用量日志和订单也回填 credits 快照；迁移后不再用当前汇率重算历史。

### 第三步：shadow

新代码同时计算 USD 和 credits：

- 继续按旧 USD 路径扣费；
- 同步写 shadow credits 和对比日志；
- 每天对账：余额、usage_logs、API Key quota、订阅用量。

关注差异来源：舍入、历史数据、赠送、退款和跨窗口用量。

### 第四步：切换

把 `engine` 从 `shadow` 改为 `credits`。所有正在处理的请求在请求开始时读取一次 `engine` 和配置快照，避免同一请求前后使用不同单位。切换后旧路径保留只读一段时间，便于排查和回滚。

### 第五步：清理

至少经过一个完整账期后，再考虑隐藏旧字段和旧 API 字段。不要在第一版直接 drop 列。

## 8. 高可用要求

1. PostgreSQL 是唯一账本真相，Redis 只做缓存和预扣。
2. 余额扣减使用条件 UPDATE 和事务，不能依赖进程内锁。
3. ledger 使用唯一幂等键，异步 billing worker 可安全重试。
4. 汇率配置使用短 TTL 的内存快照；数据库不可用时沿用最后一次有效值，不用默认值偷偷改价。
5. 配置更新使用版本号，订单和用量日志保存版本号与汇率快照。
6. 每日自动对账并报警：`SUM(ledger.delta_credits) = 余额变化`，`usage.charge_credits = 扣费流水`。
7. 迁移期间保持旧字段可回滚；灰度期间不同实例必须同时支持两种单位。
8. 不在请求热路径调用银行、支付商或第三方汇率接口。

## 9. 推荐实施顺序

按风险从低到高：

1. 配置、积分转换函数、ledger 和 schema。
2. 用户余额、usage billing、API Key 配额。
3. 订阅额度和平台限额。
4. 充值、退款、兑换码、返利。
5. 前端统一格式化和管理端配置。
6. shadow 对账、灰度切流、清理旧字段。

第一版建议只支持“全站统一展示币种”，不做每个用户独立 USD/CNY 钱包。这样可以覆盖大多数人民币站点，代码量和故障面最小。以后如果确实需要双币钱包，应在 ledger 上增加 `currency` 或 `bucket_currency`，而不是把当前单积分账本硬拆成两套余额。

## 10. 与 NewAPI 的对应关系

NewAPI 的做法值得借鉴：

- 内部 `quota` 固定，页面显示 USD/CNY/自定义货币。
- `USDExchangeRate` 控制展示换算。
- 充值售价使用独立的 `Price`，不与展示汇率混用。
- 余额、日志和充值最终都落到 quota。

本方案对应为：

```text
NewAPI quota                 -> Sub2API credits
NewAPI QuotaPerUnit          -> credits_per_usd
NewAPI USDExchangeRate       -> usd_to_cny
NewAPI Price                 -> recharge_rates["CNY"]
NewAPI quota_display_type    -> display_currency
```

Sub2API 需要额外补齐的是：整数账本流水、原子幂等扣费、汇率快照、订阅/平台配额统一使用 credits，以及全页面统一格式化。

## 11. 需要重点验证的场景

- 同一 request_id 重试不会重复扣费。
- 余额刚好等于一次请求费用时不会扣成负数。
- 并发请求下余额、API Key 配额和 ledger 一致。
- 切换 USD/CNY 后余额显示变化，但 `charge_credits` 和余额不变。
- 改汇率不会重算历史订单和历史用量。
- 充值订单重复回调只到账一次。
- 退款按原订单 credits 冲正，不按新汇率。
- 人民币支付、美元支付、赠金、折扣和优惠券金额一致。
- 文本、图片、视频、搜索、音频、批量任务都走同一个转换入口。
- 迁移前后用户购买力一致，旧字段和积分字段可对账。

