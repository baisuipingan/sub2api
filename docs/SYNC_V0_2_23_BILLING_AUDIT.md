# v0.2.23 定价与积分接入独立审计

日期：2026-11-18。范围：官方 `v0.2.22..v0.2.23` 的 token 定价、缓存写入计量与扣费，目标为 `codex/sync-v0.2.23` 的合并工作树。

## 整合结论

官方定价和缓存修复先进入最终美元金额，再进入既有的积分命令及同笔日志；并非只对 `legacy` 模式生效。组合回归发现并修复一个共享目录回退遗漏：没有 Resolver、也没有推理等级时，`CalculateTokenCostForRequest` 曾调用只接收模型、用量和倍率的 `CalculateCost`，丢弃请求服务档位和定价时刻，造成 Ultra Fast 按标准档计算。正常生产 Wire 注入 Resolver，不走该分支；兼容和内部调用仍应正确，故统一该入口为 `CalculateCostUnified(tokenCostInput(...))`，删除所有结果相同的条件分支。未改动既有钱包、订阅或批量结算生产实现。

### 官方变化如何进入定制链路

| 变化 | 实际接入点 | 定制行为 |
| --- | --- | --- |
| GPT-6.2 Sol/Astra 单价、Fast/Ultra Fast、促销截止时刻 | `BillingService.getModelPricingAt`、`applyModelSpecificPricingPolicyEx` 和 `openAIUltrafastMultiplier`；统一 `CostInput.PricingAt` | 两套 RecordUsage 都携带请求开始时刻；延后结算不按当前时间重新决定 3×/6×；原有订阅高峰和分组倍率在得到 token 成本后叠加 |
| 渠道 `ultrafast_multiplier` | `ChannelModelPricing` → repo/handler/service → `ModelPricingResolver.applyTokenOverrides` / `GetModelPricingWithChannel` → `configuredServiceTierMultiplier` | 显式渠道倍率优先于官方默认和促销；Fast/Flex 使用自己的独立倍率，不会被 Ultra Fast 覆盖 |
| Anthropic ↔ OpenAI 互转保留缓存 TTL | `apicompat` 的 presence 字段和 5m/1h details，`OpenAIUsage.billingUsageTokens` 和 `ClaudeUsage.billingUsageTokens` | 两套使用记录入口都把 TTL 明细送入同一 token 计价管线；OpenAI 输入总量拆为互斥的普通输入、读取和写入，原生 Anthropic 维持互斥桶 |
| 总写入量高于 TTL 明细之和 | `allocateCacheCreationBillingTokens` | 未分类差额按 5m 收费，保留已知 1h 部分；总量为准的细分归一化仍用于防止矛盾明细额外加算 |
| 账号统计成本的 TTL | `GatewayService.recordUsageCore` 改传 `billingUsageTokens`；`calculateTokenStatsCost` 共用差额分配 | 自定义账号成本规则能看到相同的 1h/5m 明细和未分类差额；开启“应用渠道定价到账号统计”时沿用最终 `TotalCost` |
| native OpenAI 网关经 Anthropic 上游的 1h 写入 | `openai_gateway_*_anthropic_native` 与 `openai_gateway_usage` 保留 5m/1h；使用与原生入口相同的计价函数 | 不会因为外部入口是 Responses/Chat 而丢失 1h 桶、退回全量 5m |
| 流式 cache-only 更新、总量与显式零/缺失语义 | 共享 `apicompat`、raw/native response handler、Anthropic snapshot 合并函数 | 这些变化发生在最终 `Usage` 形成前，随后统一进入积分；不为 Studio 或 MiloCode 另设旧解析/旧计价管线 |

### 最终扣费边界

`OpenAIGatewayService.RecordUsage` 和 `GatewayService.recordUsageCore` 都先得到 `CostBreakdown`，然后通过 `applyUsageBilling` → `buildUsageBillingCommand` → `applyUsageBillingCreditSnapshot` 生成原子结算命令。整数积分从规范化到 USD 8 位小数的 `ActualCost` 转换，usage log 复制同一命令的积分、汇率和版本。

- `credits`：钱包或订阅、Key 额度及速率窗口均消费同一积分；账号额度仍按倍率前成本与账号自身倍率计算。
- `shadow`：保持美元扣款，并生成同一金额的积分观察快照，不启用积分桶扣款。
- `legacy`：保持美元扣款，不生成积分扣款。官方 token 修复和渠道倍率同样生效。
- 两种入口继续使用上次真正聚合的 lane/repository。此次官方未修改聚合 SQL，因此无需重写或新增第二份结算逻辑。

Studio 通过普通网关 router 执行生图，MiloCode 领取受管 Key 后使用普通 `/v1` 网关；它们不会跳过上述服务。图片按张费用保持独立图价和图片倍率，不将文本 Ultra Fast 或订阅 token 高峰强行套入按张费用。

## 新增组合验证

`backend/internal/service/billing_credit_v023_integration_test.go` 为 **unit-tag 的服务组合测试**，在真实 RecordUsage 调用后的 repository 接口捕获 command 与 log，不宣称它本身执行了真实数据库事务或收费上游请求。

54 个子场景：`3 个引擎 × 2 个入口 × 9 个定价场景`。

- Sol 和 Astra 在 `2027-03-01T08:00:00Z` 前后保持请求时点的 3×/6×；同时叠加分组 1.25× 与订阅高峰 2×。
- Resolver-free 入口也必须使用请求定价时刻。
- 渠道 Ultra Fast 显式 1.25×，在促销前后都保持最终倍率，不被改写成 3×/6×。
- 缓存写入 11,000、其中 1h 3,000，剩余 8,000 按 5m；以及全部 11,000 为 1h 的对照。
- 对比两套入口的输入计数、1h 明细、写入成本、`TotalCost`、`ActualCost`、钱包/订阅 USD、Key quota/rate、账号 quota，以及对应引擎的积分快照。
- TTL 场景额外使用独立账号统计定价规则，证明账户成本的 1h 与差额修复没有仅进入用户售价。

54 个子场景及既有官方价格因子、渠道 Ultra Fast、GPT-6.2 请求时点和 TTL 组合定向测试已通过。`billing_token_cost_request_test.go` 另直接验证无 Resolver 时的标准/priority/flex/促销前后档位，以及未指定时刻/档位时与旧 `CalculateCost` 在负倍率、零倍率、正倍率、长上下文和 BillingMode 上的相同行为；保留原 `NoResolverFallsBackToCatalog` 回归。

命令（在 `backend/`）：

```sh
GOMAXPROCS=2 go test -tags=unit -p 2 ./internal/service \
  -run '^TestRecordUsageV023PricingReachesEveryBillingEngine$' -count=1
```

完整后端、前端、数据库迁移和其他转发专项结果由本次主合并验收统一记录。本审计不提交发布标签或操作生产。
