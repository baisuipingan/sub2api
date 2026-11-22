# 工作台对话执行

工作台使用同一个输入框处理问答、搜索和文档。浏览器提交后订阅持久化事件，并请求推进下一步。每次请求至多启动一次模型调用或一次工具操作。浏览器刷新后重新读取任务记录再推进。Go 对每一步分配租约，记录发出和完成状态。已经发出的调用失去确认时，任务结束并保留记录，避免自动重复请求。用户可以在原对话里发“继续”。

## 模型与计费

模型目录从工作台绑定的 Agent 分组读取。原生路线分别使用 `/v1/responses`、`/v1/messages` 和 Gemini `streamGenerateContent`。目录根据聚合路由和上游账号能力选择协议，兼容渠道仍可使用 Responses 或 Chat Completions。模型界面使用公开模型 ID 对应的能力档案，别名可绑定官方档案。每次模型请求使用用户已有的 Agent Key，经网关执行分组授权、模型路由、RPM、并发和用量计费。图片生成的分组单独配置。

Claude 原生 Messages 使用支持该端点的 Anthropic API Key 账号。官方 Anthropic 地址自动启用缓存。已确认支持顶层 `cache_control` 的中转渠道可在账号 `extra` 中设置 `workbench_anthropic_prompt_cache: true`。聚合分组同时接入同名 Claude 兼容账号时，可以用 `messages` 精确规则选 Anthropic，并用 `any` 精确规则保留其他端点的原路由。[Anthropic 缓存文档](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)

## 会话上下文

同一模型和配置下，工作台保留已经发给模型的消息顺序，在末尾依次追加原生工具调用、工具结果、最终回复和下一条用户消息。Gemini 保留原生签名内容段，OpenAI Responses 保留调用和加密推理项，Claude Messages 保留原生内容块。界面通过独立的公开消息和工具明细展示对话。

数据库在操作记录里为完整模型请求保留哈希，已完成的原生回执和工具结果各写一份。每轮开始时固定可复用的历史范围。任务结束 24 小时后清理重复的流式片段和原始操作输入，仍在可复用范围内的模型回执和工具结果按需保留。达到预算时按完整轮次压缩历史，保留近期消息和文件版本，随后从这段历史继续追加。模型、工具配置或选中答案变化后，用可公开回放的对话和文件引用重建受影响的上下文。

使用记录里的缓存读写 token 反映上游回报和渠道计费策略。验证前缀时在同一模型、账号和配置下逐次比较网关出口，再看下一轮首调的缓存读取和对话总费用。[OpenAI 缓存文档](https://developers.openai.com/api/docs/guides/prompt-caching) · [Gemini 缓存文档](https://aistudio.google.com/docs/caching?generate-content=true)

## 搜索和文档

工作台搜索使用管理端的独立供应商设置。搜索和网页读取使用服务器的地址校验，并从成功的工具记录生成引用。用户和管理员都可以逐条展开工具过程，查看执行状态、耗时和操作摘要。

在「工作台管理 → 算力节点」登记独立办公工具服务。现有单节点部署可沿用 `OFFICE_AGENT_TOOLS_URL` 和 `OFFICE_AGENT_TOOLS_TOKEN`。办公节点处理 Word、Excel、PowerPoint 和 ZIP 批改，按文件版本缓存输入，用本地临时目录保存过程文件。修改后的文件重新读回校验，成品通过短期签名上传到工作台的私有对象存储。Sub2API 负责权限、任务状态、计费和文件版本。模型可以读回成品再继续修改。Markdown、纯文本、CSV、JSON、Python 源码和 Draw.io 的结构化文件操作由 Go 处理。未接入工具服务时，原有的基础 DOCX 操作仍可使用。

消息交付区和文件库复用文件预览。已有 PDF 或图片预览直接显示，Word 和 Draw.io 可在浏览器中按需预览。Word 文件的打印版式由用户主动发起，办公节点生成 PDF 并按源文件版本保存。打印版生成期间仍可使用快速预览和下载。旧对话、任务和已交付文件沿用原记录。新任务按操作绑定节点，浏览器离开时，Sub2API 会收尾已经派发的文档操作。
