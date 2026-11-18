# v0.2.23 协议转换、转发与定制入口审计

日期：2026-11-18。官方范围：`v0.2.22..v0.2.23`；工作分支：`codex/sync-v0.2.23`。本记录只覆盖转发与协议范围，不替代积分账务、渠道定价、迁移及前端的独立审计。

## 结论

未发现本次合并造成的转发阻断或定制链路遗漏。本范围无需修改生产实现。官方本次修改的协议转换、OpenAI 流式处理、GPT-6.2 元数据与模型转换文件，合并后与 `v0.2.23` 一致；这项文件比对只是保留实现的证据，下面另追踪了 fork 实际入口，并执行对应行为测试。

## 官方变化与实际接入

| 范围 | 核对内容与结果 |
| --- | --- |
| Anthropic 缓存写入及读取用量 | `AnthropicUsage` 和 `AnthropicPromptTokensDetails` 的 JSON 编解码区分缺失、null、显式 0；TTL 明细使用指针保留增量语义。`mergeAnthropicUsage`、`mergeSSEUsagePatch` 和 native 路径的 `claudeUsageToOpenAIUsage` 保留 aggregate 与 5m/1h；缓存字段单独更新时会重算已知 prompt total，不再覆盖独立普通输入。 |
| 协议互转的账务边界 | `GatewayService` 的 Responses/CC 转换与 `OpenAIGatewayService` 的原生 Anthropic、Responses/CC 桥接都使用新 usage 归一逻辑。缓冲 SSE 也归一最终 usage，不依赖本次增量恰好包含普通 input/output。对客户端输出按目标协议表达汇总缓存字段；内部转发结果保留用于本次网关计费的 TTL 5m/1h。并未承诺任意第三方兼容网关会继续传递非标准 TTL 明细。 |
| TTL 覆盖 | `applyCacheTTLOverride` 使用权威 aggregate；缺失 aggregate 时才使用明细合计。重写 JSON 和内部 billing usage 分开验证：原生透传保留上游 wire，不把配置后的内部计费投影冒充成上游实际返回。 |
| OpenAI 首次输出前 idle | 主 Responses 流式处理改为 Timer，并按最近扫描到的上游数据重新计算剩余 idle 时长；首个语义输出前超时返回可安全重试的 `UpstreamFailoverError`，包含只有 keepalive 已写出的情况。沿用 handler 的 `maxOpenAIFirstOutputTimeoutSwitches=1`，与首输出超时共用一次换号预算。已输出正文后不能重放其他账号答案，仍返回流内超时错误。此项只覆盖使用该处理器的链路，不能泛化成所有协议、非流式请求都有同一个 idle 策略。 |
| 原生 Chat Completions flush | 新 `flushBeforeUpstreamRead` 在 scanner 必须等下一次上游读取前 flush，合并同一次读取内多行；EOF 时也会 flush。已有 usage 提取、客户端断开后的用量收尾、截断错误归属、model 还原和 response-model 观察均在相同处理器内保留。 |
| DeepSeek / Ollama 请求改写 | 只收集需要补全的 assistant 消息，调用 `setStringFieldInJSONObjects` 一次拷贝写入；定位不可靠时回退原来的逐条 `sjson`。两处调用仍在原模型/账号/协议条件内，不改变非目标请求。官方测试逐字节对比旧写法，并覆盖 Unicode、转义、重复键、空对象、长工具对话和随机消息。 |
| GPT-6.2 | 增加 Sol/Astra 显式身份、effort/compact 别名、官方 Codex 离线元数据、提示词与工具能力。公开别名经过账号映射后再校验真实上游模型的参数；`none`/`minimal` 拒绝，适用链路移除不支持的 sampling，工具调用要求 Responses 时不会静默降级到 CC。 |
| Ultra Fast 订阅权利 | `gpt62PlanIncludesUltrafast` 与账号计划选择器使用同一 SKU 别名：Pro 200 只为 Sol、Pro 500 为两款提供默认权利；显式上游 discovery grant 保留。模型 manifest 与 group-forced tier 共用判定；没有权利且只开 Ultra Fast 的 OAuth 账号不被强推，若另开 Fast 则沿用 priority。API Key 和原有模型的处理保持官方策略。 |

## fork 链路核对

- **MiloCode**：`DesktopAgentService` 下发 `openai-responses` provider、用户 managed Key 和站点 base URL。模型调用继续进入公共 OpenAI handler/service，未另建 desktop 转发分叉，因此同样经过新 GPT-6.2 参数策略、首输出保护和协议转换。能力档案内容的更新由另一审计范围检查，本记录不把网关认识新模型等同于远端档案已发布。
- **组合分组与自我代理**：公共 handler 先解析分组、渠道与公开模型，再进入共享转发。fork 的 `preserveOpenAIAPIKeySessionHeader` 仍同时在常规和 passthrough 两个出站构造器执行；显式会话信号可跨兼容网关保留、内容变化不重新生成会话，OAuth/Codex 隔离保持原样，不把逐请求 ID 当会话。WebSocket 组合别名、admission、渠道计费与模型切换边界保持现有实现。
- **图片工作台**：`StudioJobService.buildStudioGatewayRequest` 使用用户 managed Key 生成 `/v1/images/generations`、`/v1/images/edits` 或 Gemini 原生请求，worker 的 `s.router.ServeHTTP` 进入完整公共 router。没有另一套旧调度/转发/计费服务；本次文本 SSE 的 idle/flush/DeepSeek 优化不宣称改善非流式生图时长。任务的 dispatch 检查点和“不明确结果不再自动重复付费调用”的规则未改。
- **模型与上游留痕**：观察器在改写回公开模型名之前读取真实上游 response model；`UpstreamModel`、`UpstreamResponseModel`、冲突标记、实际 endpoint/tier 等字段仍各自保留。每次 attempt 的观察器重置、terminal 胜出和 response tier 独立于 request tier 的语义有既有回归验证，不将失败账号的模型或请求参数错写成成功上游事实。

## 验证

已通过定向 `go test -tags=unit -p 2`，日志：

- `output/sync-v0.2.23-forwarding-unit.log`：apicompat、service、handler 的缓存存在性/TTL/转换边界、prompt total、原生 Anthropic 路由到账务边界、idle 前后输出及 keepalive、raw CC flush、GPT-6.2、JSON 单遍改写与模型观察测试。
- fork 会话头保留、组合 WebSocket、原生流截断/客户端断开、公开模型还原及失败 attempt 响应头隔离已逐项检查对应既有回归。第二轮定向执行在编译阶段由本审计主动取消，以减少与主进程全量 unit 的重复编译；`output/sync-v0.2.23-forwarding-fork-regression.log` 不能作为通过证据。这部分执行结果以主进程最终全量 unit 日志为准。

首组命令中 `internal/pkg/openai` 包因筛选表达式未命中测试而只完成编译，不将它记为该包独立用例通过；GPT-6.2 识别及官方元数据的行为测试实际位于 `internal/service/openai_gpt62_test.go`，已执行。

未启动数据库容器、未改生产、未调用收费模型、未改 ZCode 仓库。本地行为验证不代表所有供应商线上端到端请求均已重新执行。
