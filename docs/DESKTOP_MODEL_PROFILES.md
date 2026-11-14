# 桌面模型官方档案与执行能力

2026-11-14 修正。本合同替代此前“从已保存配置导出能力模板”的设计。旧能力由管理员随填，不能作为可信事实复用。本轮不改变模型网关、钱包、计费或 ZCode 引擎。

## 来源与范围

内置档案位于 `backend/internal/service/desktop_agent_profiles.go` 与 `desktop_agent_vendor_profiles.go`，随代码发布，运行时不联网探测。`GET /api/v1/admin/desktop/profiles` 仅返回内置档案，不再读取已保存模型的能力值。每项包含官方来源 URL、核对时间、修订号、官方规格、当前执行器投影及限制说明。

### 2026-11-14 多厂商能力档案扩展

在已有 7 项 OpenAI 档案与官方来源校验基础上，新增 DeepSeek、Anthropic Claude、xAI Grok、Google Gemini 的文本 / 编码 Agent 档案。后端注册表是唯一能力来源；管理页读取同一份目录，已知公开 ID 自动关联，自定义名仍显式选择，不按前缀继承能力，不追溯供应商私有模型名，也不更改公开路由 ID。

管理端同时校验档案的 `source` 与对应官方文档域名，不能仅放开为任意 HTTPS。新增档案逐项核对官方公开 ID、文档明确说明的别名和能力，未知供应商自定义名由管理员显式关联。官方推理强度与桌面 Responses 执行链实际可发送的强度分开记录；预算式 thinking 或没有公布离散档位的模型，其管理元数据 `official_reasoning_levels` 使用空数组，不能虚构官方枚举。可执行 `reasoning_levels` 仍必须非空并通过既有协议校验。`none` 仍表示省略 effort、采用上游默认，不冒充显式关闭思考。生图、音频及特殊多 Agent 接口不属于桌面文本执行目录。

本轮不增加 bootstrap 字段、协议版本或数据库迁移，不调整网关路由、转换与计费，不写入管理员当前配置，也不向模型上游发起付费探测。已有有效模型、配置覆盖与精确别名边界保持原合同。仅开放现有 Responses 执行器和网关转换可表达的能力；不能保持语义的能力关闭并在管理页解释。共享输入 / 输出上下文的厂商按上下文减最大输出计算保守输入预算，界面明确这是派生预算，而不是厂商单独公布的最大输入限制。

首批按实际已确认模型和同协议常用系列核对：

| 档案 | 官方上下文 / 最大输入 / 最大输出（Token） | 官方推理档位 |
| --- | --- | --- |
| [GPT-6.1 Sol](https://developers.openai.com/api/docs/models/gpt-6.1-sol) | 1,050,000 / 922,000 / 128,000 | low、medium（默认）、high、xhigh、max |
| [GPT-6 Astra](https://developers.openai.com/api/docs/models/gpt-6-astra) | 1,050,000 / 922,000 / 128,000 | low、medium、high、xhigh、max |
| [GPT-6 Sol](https://developers.openai.com/api/docs/models/gpt-6-sol) | 1,050,000 / 922,000 / 128,000 | none、low、medium（默认）、high、xhigh、max |
| [GPT-6 Luna](https://developers.openai.com/api/docs/models/gpt-6-luna) | 1,050,000 / 922,000 / 128,000 | none、low、medium（默认）、high、xhigh、max |
| [GPT-5.6 Sol](https://developers.openai.com/api/docs/models/gpt-5.6-sol) | 1,050,000 / 922,000 / 128,000 | none、low、medium（默认）、high、xhigh、max |
| [GPT-5.6 Terra](https://developers.openai.com/api/docs/models/gpt-5.6-terra) | 1,050,000 / 922,000 / 128,000 | none、low、medium（默认）、high、xhigh、max |
| [GPT-5.6 Luna](https://developers.openai.com/api/docs/models/gpt-5.6-luna) | 1,050,000 / 922,000 / 128,000 | none、low、medium（默认）、high、xhigh、max |

### 新增厂商目录与执行限制

新增 30 项，共 37 项档案。下表只记录文本 / 编码 Agent；不是向管理员自动添加模型或改写已保存设置。

| 厂商 | 新增官方档案 | 当前可执行推理控制 |
| --- | --- | --- |
| DeepSeek | `deepseek-flash`、`deepseek-v4-pro` | low / high / max，默认 high |
| Claude 当前系列 | `claude-fable-5-2`、`claude-opus-5-5`、`claude-sonnet-5-5`、`claude-haiku-5-5` | low / medium / high / xhigh / max；复用已有原生 Messages 兼容桥的 adaptive 映射 |
| Claude 常用旧系列 | `claude-opus-4-6`、`claude-sonnet-4-6`、`claude-opus-4-5-20251101`、`claude-sonnet-4-5-20250929`、`claude-haiku-4-5-20251001` | 自动，省略参数；现有 legacy thinking budget 映射不等同官方 effort，32K 预算还可能超出 16K 默认输出 |
| Grok | `grok-4.7`、`grok-4.6`、`grok-4.5`、`grok-4.3`、`grok-build-0.1`、`grok-4.20-0309-reasoning`、`grok-4.20-0309-non-reasoning` | 4.7 / 4.6 为 low / medium / high / xhigh；4.5 为 low / medium / high；4.3 为 low / medium / high / xhigh；其余自动 |
| Gemini 4 | `gemini-4-carbon`、`gemini-4-argon`、`gemini-4-hydrogen` | 自动，省略参数 |
| Gemini 3 / 2.5 | `gemini-3.8-flash`、`gemini-3.6-flash`、`gemini-3.5-flash-lite`、`gemini-3.1-flash-lite`、`gemini-3.1-pro-preview`、`gemini-3-flash-preview`、`gemini-2.5-pro`、`gemini-2.5-flash`、`gemini-2.5-flash-lite` | 自动，省略参数；不将 Chat / Messages 的 thinking 映射冒充 Responses 的保真控制 |

核对依据与预算：

- **DeepSeek：** [模型元数据](https://api-docs.deepseek.com/api/list-models/)、[Responses API](https://api-docs.deepseek.com/api/create-response/)、[公开别名](https://api-docs.deepseek.com/quick_start/pricing/)。共享上下文 1,048,576，最大输出 393,216，派生输入 / 客户端预算 655,360。Flash 开图片输入和对话中系统更新；Pro 只开文本输入且不支持对话中系统更新。原生 Responses 支持工具、流式、JSON Schema；PDF 需要先上传取得 file ID，当前内联文件方式不开放。Flash 精确别名为 `deepseek-v4-flash` 和 `deepseek-v4-flash-vision-exp`，不将已退役的 `deepseek-chat` / `deepseek-reasoner` 当别名。
- **Claude：** [官方模型目录](https://platform.claude.com/docs/en/models/overview)、[effort](https://platform.claude.com/docs/en/build-with-claude/effort)、[共享上下文](https://platform.claude.com/docs/en/build-with-claude/context-windows)。当前系列及 4.6 为共享 1,000,000、输出 128,000、派生输入 872,000；4.5 为共享 200,000、输出 64,000、派生输入 136,000。三个 4.5 短名称只按官方明确别名关联其日期版本。图片、内联 PDF 可经已有 Responses→Messages 桥传递；桥会前置 system 消息且未转换 `text.format`，因此对话中系统更新与 JSON Schema 保守关闭。官方 OpenAI SDK兼容层本身会忽略 reasoning_effort，不能据它承诺原生 effort；当前新系列 effort 能力依据本项目已有原生桥。Sonnet 4.5 等旧版本仍受厂商弃用和退役策略限制，档案不会让退役模型重新可用。
- **Grok：** 各档案来源指向 [官方逐模型规格](https://docs.x.ai/developers/models)，另核对 [Responses 参数](https://docs.x.ai/developers/rest-api-reference/inference/responses.md) 与 [推理说明](https://docs.x.ai/developers/model-capabilities/text/reasoning.md)。4.7 / 4.6 / 4.5 上下文 500,000，4.3 与 4.20 为 1,000,000，Build 为 256,000。官方 Responses 的输出 128,000 是默认值而非模型硬上限；本项目以 128,000 作为保守输出 cap，Build 使用 64,000，均从共享上下文扣除再下发输入预算，界面明确说明。支持图片、工具、流式及 JSON Schema；PDF 的上传 / 检索协议不等同当前内联 PDF，保持关闭。4.5 的 xhigh 在通用指南映射成 high，暂不提供独立档位；4.3 的显式 none 受现客户端省略语义限制，不开放。
- **Gemini：** [官方模型目录](https://aistudio.google.com/docs/models)、[thinking](https://aistudio.google.com/docs/thinking)、[OpenAI 兼容协议](https://aistudio.google.com/docs/openai)。Gemini 4 共享上下文 1,048,576，同步输出 65,536，派生输入 983,040；长 decode 的百万输出不属于当前协议。其余所列型号文档独立给出最大输入 1,048,576、输出 65,536，客户端采用该输入上限。只保守开放文本、图片、工具、流式，不假设音频 / 视频 / PDF / 原生搜索或 JSON Schema 可经当前路径保真。Gemini 原生账号的 `generateContent` 不是桌面 Responses 协议；需使用已兼容 Responses 的上游或现有 Antigravity 桥。Antigravity 的 effort 会折叠档位，故内置只提供自动。2.5 Pro / Flash 已弃用，当前官方仍为既有用户保留至 2026-12-01，之后自动迁移；2.5 Flash-Lite 受既有用户可用性限制。不发明 `gemini-3.1-pro` 这类去掉 preview 的别名。

仅精确匹配上述官方 ID，以及官方文档明确列出的别名。不靠前缀猜测，不把 `openai/xxx` 或供应商后缀自动当成官方 ID。自定义公开名可以显式关联档案，关联后仍使用原公开 ID 路由，不回溯供应商私有名。档案声明的能力要求渠道确实支持当前 Responses 接口或对应已有兼容桥；增加档案不会凭空新增厂商原生协议。

## 官方能力与实际可用能力

上表原有 OpenAI 档案均支持文本/图片输入、文本输出、流式、工具调用、结构化输出；音频/视频输入保持关闭。其 PDF 支持依据官方 [File inputs](https://developers.openai.com/api/docs/guides/file-inputs) 与现有 Responses 文件序列化能力；对话中系统消息依据 [Responses 输入消息合同](https://developers.openai.com/api/reference/resources/responses/methods/create) 及现有执行器兼容转换。其它厂商按各自段落的保守执行投影，不批量复制 OpenAI 能力开关。

以下差异必须呈现，不能把官方能力未经适配全部置为启用：

- **上下文预算：** 当前引擎没有独立的最大输入字段，压缩时最多预留约 21K 输出与 13K 缓冲，不会固定预留物理 128K 输出；直接下发 1.05M 会允许输入超过 922K。故客户端 `context_window` 保守使用 922,000；官方 1,050,000 上下文与 922,000 最大输入另列为来源信息，界面称“客户端上下文预算”。这不是官方上下文缩水，也不修改引擎。
- **原生网页搜索：** 官方支持，但当前 ZCode provider-native WebSearch 编码器仅支持 Anthropic。Responses 档案暂将 `supports_native_web_search=false`。这不影响独立浏览器工具。
- **none：** 既有桌面协议把 `none` 编码为省略 effort，含义是采用上游默认，不能等同于官方显式 `reasoning.effort:"none"`。内置可执行档位先只发布 low/medium/high/xhigh/max；官方档位仍完整列出。高级覆盖中的 none 文案明确说明省略参数。
- **MFJS：** 属于特定模型的工具结构兼容处理，OpenAI 档案保持关闭。
- **运营默认：** 新档案的单次输出上限 16,384、默认推理 medium 是本项目运行默认，不能冒充官方推荐费用预算；管理员可调整合法范围内的运营默认。

## 旧数据、别名与覆盖

配置仍保存于一条 `desktop_agent_config_v1`；新增可选管理元数据：

```json
{
  "model_profiles": {
    "my-public-model": {"profile_id":"gpt-6.1-sol","override":false}
  }
}
```

元数据不加入 bootstrap 模型字段，不改变 protocol 1/2/3 的严格线协议。管理员配置响应可带绑定，发布给客户端的仍是既有完整模型结构。

- 已知官方 ID 的旧数据：读取和执行时重新投影内置能力，保留公开 ID、展示名及仍合法的运营默认；非法旧默认退回档案默认。读取不自动写数据库，管理员保存后才物化新配置。旧布尔能力、上下文和物理输出等随填值不会继续下发。
- 未关联的旧自定义 ID：管理页保留可见；不能继续把旧声明当真实能力发布。v3 将该模型隔离到现有 `model_unavailable`，其它有效模型可继续使用；v1/2 保持整体拒绝。管理员需一次性选择可信档案。
- 新增已知 ID 自动套用；新增自定义 ID 须显式关联。选择档案不能改模型 ID、展示名或默认模型归属。
- `override:false` 时高级能力只读；服务端也重新生成基线，不能靠伪造表单字段覆盖。只有显式保存 `override:true` 才保留管理员高级声明。恢复档案默认会清除此标记。覆盖是管理员对特殊上游差异的声明，不等于官方认证。
- 失败的档案读取阻止发布，保留草稿并允许独立重试，不能回退为旧声明模板。绑定和模型由同一草稿所有者一起更改、校验和保存。

不引入新表或迁移。回退旧二进制前，应注意旧版本严格解析器不认识新增配置字段；正式发布与回退须按交付说明执行。

## 验证重点

官方 ID 修正旧随填能力、未知别名隔离、显式关联保留路由名、override 的服务端边界、精确别名匹配、档案对象不共享可变状态、读取不写库、不领取 Key、bootstrap 不泄漏新增管理元数据；以及前端原子草稿、重试不丢编辑、覆盖与恢复、窄屏与多语言。

### 2026-11-14 验证记录

- `go test -tags=unit ./internal/service -run 'TestDesktopAgent' -count=1` 通过。新增回归涵盖全部厂商来源、别名唯一 / 精确识别、Flash / Pro 媒体隔离、不同型号预算、可变引用隔离、自定义公开 ID，以及 protocol 1/2/3 响应形状不增加管理元数据。
- 前端 122 项相关测试、定向 ESLint、类型检查和完整生产构建通过；最终文案修订后再通过 locale 完整性与编译检查。构建只出现既有警告。
- 从实际 Go 注册表导出完整 37 项 JSON，真实 Chromium 使用本次 Vue 卡片、Select、样式及严格档案解析器验收。确认 DeepSeek 可执行档位仅 low / high / max，选择 max 能更新运营默认；37 项列表完整可选，自定义别名选择 Claude 后不改变公开 ID；Grok、新旧 Claude、Gemini 精确 ID 自动关联；Gemini 仅自动档。
- 已人工查看中文桌面、中文暗色 390px、英文暗色 390px 截图，未出现横向溢出或未翻译的 key。最终浏览器验收过程没有脚本或控制台错误，证据保留于 `output/playwright/agent-vendors-*`。临时入口、服务与导出辅助测试在验收后删除；没有使用生产凭据、写生产配置或调用付费模型。

本轮尚未发布生产。档案由服务端下发，部署本次前后端后即可使用现有 MiloCode 3.17.3，无需为此次目录扩充重新构建安装包。
