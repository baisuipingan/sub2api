# v0.2.22 调度与转发合并审计

审计日期：2026-11-17。官方基线：`v0.2.22`（`b0fa9f3e6`）；合并基线：`8d15af77c`。本次审计覆盖调度、请求体、流式响应、组合分组，以及定制工作台和桌面端是否实际进入这些公共路径；计费批处理由独立审计覆盖。

## 结论

没有发现因定制功能绕过官方新版调度/转发优化的情况。官方调度快照合并加载、共享元数据、请求体复用、首输出前 keepalive、首输出暂存降级、组合路由公开模型名还原均已进入实际调用路径。既有 Studio 和 MiloCode 使用公共网关，不需要再复制一套实现。

发现并删除了一处自动合并留下的重复赋值：`AccountTestService.testOpenAIImageAPIKey` 连续两次把同一个 `OpenAIImageTestSize()` 写入 `payload["size"]`。这不改变原有结果，但应清理掉。未发现其他需修复的阻断问题。

## 按调用链检查

| 检查项 | 实际代码路径与证据 | 结果 |
| --- | --- | --- |
| OpenAI 候选账号是否使用新版快照 | `backend/internal/service/openai_gateway_scheduling.go` 的 `listSchedulableAccounts` 调用注入的 `SchedulerSnapshotService.ListSchedulableAccounts`，正常选号和重选共用此路径。 | 通过 |
| Anthropic/Gemini/组合分组是否绕过快照 | `backend/internal/service/gateway_scheduling.go` 的 `listSchedulableAccounts` 使用同一快照服务；组合分组按解析后的具体平台进入该服务。 | 通过 |
| 缓存 miss 是否按 bucket 合并回源 | `backend/internal/service/scheduler_snapshot_service.go` 的 `loadFallbackAccounts` 使用 `fallbackLoads.DoChan(bucket.String())`；共享回源使用 `context.WithoutCancel` 后再施加内部超时，单个等待者取消不取消其他等待者。 | 通过 |
| 热路径是否仍整组反复深复制/解码 | `backend/internal/repository/scheduler_cache.go` 的 `GetSnapshotValues` 复用已解码元数据，以调用独占的 Account 值切片返回；服务优先使用 values-reader，并原地压缩不可调度账号。`Account.PrepareSharedSnapshot` 预解析模型映射；定制会话头逻辑没有改动此协议。 | 通过 |
| 精简快照是否漏掉影响选号的字段 | 官方 `buildSchedulerMetadataAccount` 保留 `openai_capabilities`、`openai_passthrough_model_filter`、生图尺寸档位、Grok 媒体资格、组归属、限额和 RPM 字段；对应单测覆盖这些字段。 | 通过 |
| 组合路由是否破坏请求体复用 | `backend/internal/server/routes/gateway.go` 中间件读一次请求体，仅在确需改写时改顶层 `model` 或 Live 的 `session.model`；`backend/internal/pkg/requestmodel/requestmodel.go` 的 `ResetRequestBody` 用 `httputil.NewPrereadBody` 回填。后续 `ReadRequestBodyWithPrealloc` 返回同一切片。 | 通过 |
| 普通 handler 是否仍用优化后的 body reader | `gateway_handler.go`、`openai_gateway_handler.go` 通过 `request_body_limit.go` 调用 `ReadLenientJSONRequestBodyWithPrealloc`；`backend/internal/pkg/httputil/body.go` 保留官方内存预算、压缩解码和 PrereadBody 快速路径。 | 通过 |
| 首输出前 keepalive 是否可见 | `openai_gateway_response_handling.go`、`openai_first_output_timeout.go` 保留官方实现；测试确认上游 keepalive 能先于内容发出，后续仍可切换账号，keepalive 不重置首输出超时。 | 通过 |
| 首输出暂存失败是否仍能安全工作 | `openai_first_output_timeout.go` 保留官方 spool 创建失败转内存、暂存上限及错误处理；测试覆盖临时文件失败、暂存溢出、尝试头/暂存前导事件隔离。 | 通过 |
| 兼容协议出错后是否错误重试 | `openai_gateway_messages*.go` 与 `openai_gateway_chat_completions*.go` 保留官方首内容前后界限：未释放内容可以 failover，释放内容后不再切换并拼入另一份答案；错误后的账号副作用也有回归测试。 | 通过 |
| 组合路由响应是否泄漏/误改模型名 | `composite_platform.go` 及相关响应转换保留官方公开模型名恢复逻辑；测试覆盖直通、Responses、Chat、流式 message_start，以及工具内容等非模型字段不被替换。 | 通过 |
| MiloCode 是否走另一个旧代理 | `desktop_agent.go` 的 bootstrap 返回站点 BaseURL 与用户受管 Agent Key，`desktop_agent_handler.go` 负责配置和凭据，不代理模型请求；客户端模型调用仍进入公共 `/v1` 网关。模型目录加载属于元数据访问，不是另一套实际转发。 | 通过 |
| 云端工作台是否绕开鉴权、调度、限额 | `image_studio_jobs_gateway.go` 构造 `/v1/images/generations`、`/v1/images/edits` 或 Gemini `generateContent` 请求；`image_studio_jobs_worker.go` 经 `s.router.ServeHTTP` 调用同一个 Gin router，使用用户受管 Key。 | 通过 |
| 服务退出时是否漏掉新版/定制服务 | `backend/cmd/server/wire.go` 和生成的 `wire_gen.go` 同时保留官方 lane combiner 与 Studio、图库、工单附件、桌面目录服务的 cleanup；应用层清理完成后才关闭 Redis/Ent。Studio 的 `Stop` 等待 worker 退出后再清空 router，并有专门回归测试。 | 通过 |
| 定制远程模型档案是否阻塞启动 | `desktop_agent_catalog.go` 的 `Initialize` 只读取持久缓存；`Start` 在 goroutine 内做远端刷新。官方异步定价启动逻辑保留。 | 通过 |

## 与官方的有意差异

- `openai_gateway_forward.go`、`openai_gateway_passthrough.go` 各额外调用一次 `preserveOpenAIAPIKeySessionHeader`；helper 位于 `openai_gateway_scheduling.go`。它只为普通 API Key 账号保留客户端已提供的会话标识；不自行生成会话，不覆盖已有 session/conversation，不作用于 OAuth/Codex。这样多级网关仍能沿用同一会话与上游缓存。该差异不替换官方调度或流式实现。
- Studio 的图库、持久任务、参考图读取和归档是定制功能，但正式模型请求经公共 router。图片编码/multipart 组包本身会分配内存，不能宣称整条图片链路零拷贝。
- Grok 定制保留图片分辨率计费元数据、按最终编辑模型限制参考图数量等行为；官方新增 xAI 错误解析逻辑仍在，未被定制覆盖。
- 官方 first-output/keepalive 优化针对适用的流式文本响应。Studio 的非流式生图任务不会因此变成流式，也不会因 keepalive 提前返回完成结果。

## 验证记录

以下命令均在 `backend/` 执行，均通过：

```sh
go test -tags=unit ./internal/service \
  -run 'Test(SchedulerFallbackConcurrentMissesShareOneDBLoad|SchedulerFallbackCanceledWaiterLeavesSharedLoadRunning|SchedulerSnapshotListUsesValuesReaderWithoutCopy|SchedulerSnapshotValuesReaderFiltersTimeBlockedAccounts|OpenAINativeStream|OpenAIFirstOutput|CompositeRoute|RestoreCompositeClientModel|ForwardAsChatCompletions_StreamError|ForwardAsAnthropic_StreamError|ForwardAsAnthropic_PreContent|StudioStopKeepsRouterUntilClaimFinishes|OpenAIAPIKeyForwardingPreservesExplicitSessionAffinity|AccountTestService_OpenAIImageAPIKey)' \
  -count=1

go test -tags=unit ./internal/service \
  -run 'Test(OpenAI.*FirstOutput|NewOpenAIFirstOutput|WriteAnthropicErrorAfterCommittedSSE|ForwardAsAnthropic_ContentReleasedNeverFailsOver)' \
  -count=1

go test -tags=unit ./internal/server/routes ./internal/pkg/requestmodel ./internal/pkg/httputil \
  -run 'Test(Composite|ResetRequestBody|PrereadBody|ReadRequestBodyWithPrealloc)' \
  -count=1

go test -tags=unit ./internal/repository \
  -run 'Test(SchedulerMetadata|BuildSchedulerMetadataAccount|SchedulerCacheSnapshotAccountIDReuse|SchedulerCacheSetSnapshotMatchesIDPublishing)' \
  -count=1
```

另通过 `git diff v0.2.22 HEAD -- <file>` 核对了表中官方实现；调度快照、Redis 快照仓储、请求体、主要 handler、流式响应、first-output 与组合路由代码未被定制替换。`openai_gateway_scheduling.go` 的差异是上述会话头 helper。

本记录证明代码调用关系及回归测试结果；未使用生产真实付费请求进行压测，不据此推断生产延迟或吞吐的具体改善幅度。计费批处理优化和全量验证结果见本次合并的独立审计记录。
