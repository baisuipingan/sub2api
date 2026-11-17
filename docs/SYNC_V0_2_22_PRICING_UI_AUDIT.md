# v0.2.22 定价、额度快照、统计展示与迁移独立审计

日期：2026-11-17。官方基准：`v0.2.22` / `b0fa9f3e6eeab1968a53e1289b69daa4bb40ca2b`。工作分支：`codex/sync-v0.2.22`，合并提交 `8d15af77c90db18485d277ecaa7af4dae600e443`。本记录覆盖独立审计负责的非转发部分；转发、调度和 billing batch 的总审计由主进程合并。

## 审计结论与变更范围

本范围没有发现需要再修改生产实现的合并遗漏。核查不仅比较冲突文件，还追踪了 fork 的 credits、云端图片工作台、MiloCode、定制 DistributionBars 到官方共享服务的实际调用关系。

主要源码入口：[官方定价服务](../backend/internal/service/pricing_service.go)、[统一计费](../backend/internal/service/gateway_usage_billing.go)、[工作台网关调用](../backend/internal/service/image_studio_jobs_gateway.go)、[工作台执行器](../backend/internal/service/image_studio_jobs_worker.go)、[桌面客户端接入](../backend/internal/service/desktop_agent.go)、[分布图](../frontend/src/components/charts/DistributionBars.vue)、[迁移执行器](../backend/internal/repository/migrations_runner.go)。

本轮仅新增/修改回归测试：

- [billing_credit_upstream_pricing_test.go](../backend/internal/service/billing_credit_upstream_pricing_test.go)：通过两个真实 RecordUsage 服务入口验证官方新价格因子进入 fork 的原子扣费命令和 usage_logs 信用快照。
- [DistributionBars.spec.ts](../frontend/src/components/charts/__tests__/DistributionBars.spec.ts)：补 3 个分布维度的测试，锁住 actual_cost 与 account_cost 的独立含义。

没有修改 billing batch、scheduler、forwarding 文件，没有提交代码、打发布标签或变更生产。

## 官方变更到 fork 调用链

| 官方变化 | 已核实的共享实现与 fork 接入点 | 结论 |
| --- | --- | --- |
| 有有效本地价格时，启动阶段远端 hash 检查改后台执行 | `pricing_service.go:414` 检查本地文件；`checkRemoteHashOnStartup:456` 后台检查，失败保留本地。`wire_gen.go:115/119/150` 只装配一个 PricingService → BillingService → ModelPricingResolver，图片预估使用同一 billing/resolver | fork 没有另起价格下载流程。该非阻塞承诺仅适用于本地价表有效且配置 HashURL；无有效本地表时仍需先获得可用价格，属于官方范围 |
| DeepSeek 显式 token 单价 override（含 0）不被默认价/峰谷策略覆盖 | `pricing_service.go` 保留 input/output/cacheRead 三字段的显式覆盖来源；`billing_service.go:1750` 只对非显式字段叠加 DeepSeek 官方高峰；`model_pricing_resolver.go` 的 group/channel 覆盖仍属最终价；账号成本调用相同价格函数、同一 request PricingAt | fork credits 只消费最终 ActualCost，未绕过新逻辑。新增组合测试验证显式 output=0 不在高峰重变成非零收费 |
| Gemini 4 Carbon / Hydrogen 价卡 | `billing_service.go:542/564` 默认/priority 价卡，`pricing_service.go` 家族别名与嵌入 JSON 同步；Carbon 独立思考档位若有明确定价优先，未配置才回基名；cache storage 为 0 | 原生与 OpenAI 兼容请求通过同一个 BillingService。MiloCode 自身能力档案原已包含 Carbon/Hydrogen，不决定价格；图片工作台不会把这两个文本模型强行当生图模型 |
| Qwen3 Embedding / Reranker 兜底定价 | `billing_service.go` 8B/4B/0.6B 精确家族识别；input-only，未知型号不套最近尺寸 | fork 没有另写 embeddings/rerank 价格表，命中共享兜底；针对性官方测试已跑 |
| Vertex us/eu 与实际区域 1.1 倍率 | `vertex_locations.go:83` 仅实际为支持的多区域端点时返回 1.1，模型回落 global 或 Express 不加价；转发结果携带 UpstreamPriceMultiplier；`gateway_usage_billing.go:1078` 仅默认 token 成本加价，搜索/cache storage 独立叠加；`account_stats_pricing.go` 默认模型文件成本同样适用，group/channel 最终价不再被覆盖 | fork 钱包/订阅/Key quota/Key rate credits 从已加价的 ActualCost 形成；新增测试验证 token 加价、storage 不加价及最终 6,557,500 credits |
| UltraFast 分组开关、模型门控与 6× 计费 | group repo/DTO/types、`api_key_auth_cache.go:114`、GroupsView create/edit/reset/save 全链完整；`admin_group.go:1096` 保存后使分组认证缓存失效，APIKey auth snapshot 有 roundtrip 测试；`openAIModelSupportsUltrafast` 与 billing 门控共用；可与 Force Fast 并存 | 定制币种显示没有覆盖新开关。新组合测试用 GPT‑6.1 Sol 带 effort 后缀，经 subscription peak 和 UltraFast 后，原子命令及 usage_logs 都是 10,500,000 credits |
| 分组日汇总不阻塞 usage_logs 写入与 retention | `custom_group_usage_rollup_repo.go:213` 注册/计算/发布分事务；计算阶段 FOR KEY SHARE，短注册/发布 FOR UPDATE；session advisory build lock 防并行重建；最多 3 轮、dirty 水位不越界；expired_before 限定汇总/尾段与触发器；retention cleanup 先推进过期下界再删源数据 | 这些名字带 custom 的文件也属于本次官方共享实现，当前与 v0.2.22 相同。`dashboard_aggregation_service.go:307` 定期/启动调用共享 Sync；`usage_log_repo_trend.go:728` GetAllGroupUsageSummary 调共享 rollup，fork 分组页面没有保留旧全表重建逻辑 |
| 按账户过滤时用户实际消费不能被账户成本替代 | `usage_log_repo_trend.go:451` 模型分布、`usage_log_repo_stats.go:835` 端点分布和 :658 总统计，SQL 分别 SUM(actual_cost) / account_cost；不因 AccountID 筛选改写行字段 | 定制 DistributionBars 的 normalize 仅读 actual_cost。模型/分组/端点三个分布新增 actual=2、account=90 的回归，均显示用户消费2 |
| 账号配额新鲜度/未知百分比、国内配额刷新 | AccountUsageCell/UsageProgressBar 当前与官方 v0.2.22 一致；utilization_unknown 显示“—”，保留失败时上次成功快照并展示刷新失败/时间；CNProviderQuotaCell 使用 probeData ?? snapshotData，snapshot 与版本变化清手动覆盖，generation 防旧账号请求覆盖新账号 | fork 账号页仍渲染官方组件；不是只合后端字段而漏前端。新鲜度/未知值组件定向测试通过；CN配额组件源码逐项复核，前序全前端 CI 已覆盖 |
| OAuth passthrough 的模型过滤开关 | Create/Edit/BulkEdit 的默认值、加载、保存及关闭透传后的字段删除均已保留；EditAccountModal 与官方唯一相关差异是先取已构建 updatePayload.extra，再取旧 account.extra，防止把本次暂存 extra 冲掉；Vertex location constants us/eu 由两个表单共享 | 创建/编辑/批量组件共185测试通过，包含 passthrough_model_filter 保存/删除断言 |
| Anthropic effort policy 与 per-turn beta 的既有配置入口 | GroupsView:641/2306 使用同一个 ReasoningEffortPolicyFields，max/over-limit/mappings 的 load/reset/create/update、类型及 auth snapshot 齐全。SettingsView:753 的 betaPolicy 仍按后端 rules 渲染，load:12527/save:12614 保留 beta_token/action/scope/whitelist/fallback。fork SettingsView 差异只在充值、工作台入口及独立管理配置，未改 beta 这段 | 本次是兼容转发执行点增强，不需要另造 fork 表单；现有控制台设置仍能配置相同规则。具体执行/请求变换由转发审计记录覆盖 |

## 三条定制链路的边界

1. **云端工作台执行**：`image_studio_jobs_gateway.go:22` 生成带用户 managed Key 的 `/v1/images/generations`、`/v1/images/edits` 或 `/v1beta/models/{model}:generateContent` 请求；`image_studio_jobs_worker.go:246` 直接调用完整 `s.router.ServeHTTP`；`server/http.go:94` 在完整 SetupRouter 后启动 worker。因此认证、分组/渠道路由、模型映射、官方转发以及 RecordUsage 均走普通网关，图库存储不自算第二套扣费。
2. **工作台价格预估**：`image_studio_pricing.go` 复用同一 BillingService/Resolver，保持分组模型图价 > 分组尺寸档位价 > 渠道图价 > 默认图价，图片独立倍率与网关一致。新增组合测试确认图片按张价格不误叠加 token 高峰。预估本身不是先验 token 报价；实际 token 生图费用仍以网关 usage 为准。
3. **MiloCode**：`desktop_agent.go:810` 下发本站 OpenAI Responses base_url；`api_key_desktop_agent.go:93` 领取指定分组的用户独立 system/purpose Key，MiloCode 后续调用公开 gateway，并不经过一套 desktop 专属模型扣费接口。新价格、group policy、quota/credits 和 usage 记录沿用共享链。没有改 ZCode 仓库或客户端二进制。

两条 RecordUsage 都在最终美元成本确定之后进入 `buildUsageBillingCommand` → `applyUsageBillingCreditSnapshot` → 原子 repo.Apply；usage_logs 使用同一命令的 ChargeCredits/汇率/版本快照。因而新官方倍率不会只记在美元列而漏掉 fork 的整数信用账。数据库批量实现的并发/去重/精度审计另见主进程记录。

## 管理与用户成本字段隔离

- 管理 DistributionBars 及其用户展开请求保持 user/key/account/group/request_type/native_compaction_v2/billing_type/upstream_model_mismatch 筛选；模型源 requested/upstream/mapping 和端点源 inbound/upstream/path 各自传递；端点额外继承总统计的 model/billing_mode 范围。
- `GetUserBreakdownStats:615` 独立聚合 actual_cost/account_cost，排序列固定白名单，fork 添加的 billing_mode/model mismatch 不影响官方实际成本修复。
- 用户页不打开 enableBreakdown，不请求管理员展开 API；前端只显示 actual_cost，`usage_handler.go:419` 服务端清掉 TotalAccountCost、端点 AccountCost、UpstreamEndpoints 和 EndpointPaths。管理页新增上游成本能力没有泄到用户页。

## 迁移与既有数据兼容

- `migrations_runner.go:25` 的主键是完整 filename；`:182` 字典序执行；`:199/203` 对 trim 后内容做 SHA256，并按完整 filename 查询。编号相同、文件名不同不冲突，不能为了编号美观重命名已发布 fork 迁移。
- 复算当前工作树全部 **304 个官方 SQL** 相对于 `v0.2.22` 的 SHA256：**0 个不一致**。
- 官方相对 v0.2.21 仅新添两个 SQL：`251_group_force_openai_ultrafast.sql` 和 `251_group_usage_rollup_nonblocking_build.sql`。前者向 groups 增 NOT NULL DEFAULT FALSE；后者向既有 usage_group_rollup_state 增 expired_before/building_until/build_dirty_from 并替换失效函数。没有删表、删列、重算余额或修改 fork 的图库/桌面/工单数据。
- fork 的 `249_api_key_type.sql`、`250_system_api_key_unique.sql`、`251_tickets.sql`、`252_ticket_read_state.sql`、`253_ticket_soft_delete.sql`、`254_ticket_attachments.sql`、`255_announcement_display_mode.sql`、`256_desktop_agent_key_purpose.sql` 及 dsapi 命名迁移保留原文件；两份新增251按完整名字独立记录。
- 默认 false 保持已有 group 的原服务档位；rollup 过期下界默认1970保持已有统计范围，再由保留策略推进。已有 usage_logs 与 credits 列语义不变。
- 前序合并验证已在临时 PostgreSQL/Redis 上跑完整 integration（见 `output/sync-v0.2.22-local-verification.txt`），覆盖迁移和 rollup 并发/清理/恢复。本轮再跑 rollup SQL mock/服务调用测试。这里没有连接生产数据库、执行真实生产迁移，也没有把“新建测试库通过”冒充为生产副本升级验收；生产备份/迁移/回滚仍按发布流程执行。

## 本轮验证证据

- service 定向组通过：PricingStartup、Deepseek overrides/peak/default/group/account-stats、Carbon、Hydrogen、Qwen3 Embedding/Reranker、Vertex 多区域与 token/storage 分离、UltraFast 门控/计费、auth snapshot、group create/update、credits 快照、model normalization/channel优先级、MiloCode profiles/catalog、图片定价顺序。
- 新增 `TestRecordUsageOfficialPricingFactorsReachCreditSnapshots` 全部4子场景通过，验证命令及usage日志：0.105 USD→10,500,000 credits；0.065575→6,557,500；0.003→300,000；0.1→10,000,000。钱包/订阅/API quota/API rate 各桶一致。
- repository/service 定向组通过：`TestUsageLogRepositoryGetModelStatsAccountFilterKeepsActualCost`、`...GetEndpointStatsAccountFilterKeepsActualCost`、分组 rollup 注册/发布/dirty/retention/锁超时/中断恢复相关 SQL mock 及后台服务调用测试。
- 前端定向9文件共 **336 个不同测试** 全通过：DistributionBars27、admin Usage24、user Usage13、Groups UltraFast7、CreateAccount51、EditAccount78、BulkEdit56、AccountUsage68、UsageProgress12。DistributionBars 在补3例后单独重跑通过；不把重复运行的24例再次计数。
- `git diff --check` 通过。前端只出现已有 Browserslist 数据过期提示，以及模拟401测试有意打印的错误日志。

## 保留的实际限制

- 未用生产账号向收费上游发真实请求，本记录证明本地源码/测试调用链和组合计费，不宣称所有供应商的在线行为已验收。
- 本次官方同步不把 MiloCode 的独立远程能力档案改造成定价文件；两者有不同用途。既有 Gemini4 档案保留官方 long-decode 最大输出1M并标 `long_decode_required`，同步 gateway 内嵌价档的 Carbon 元数据是同步输出65,536。当前客户端是 Responses 协议，没有实现专用 long-decode；这属于既有能力描述/执行协议边界，本次按用户此前“用官方最大值”的要求保留，未擅自削减档案数值。它不改变本次默认价/priority计费。
- 原子 billing batch 的性能/信用精度/订阅残差实现由另一审计范围负责；本轮只验证进入该实现前命令数值正确，未重复其数据库并发压测。
