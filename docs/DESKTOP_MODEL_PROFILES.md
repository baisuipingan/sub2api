# 桌面模型官方档案、远程目录与协议合同

本合同对应 2026-11-14 的改造，内置档案修订为 2026-11-14.2，服务已部署为 0.2.21-ds.18。当前内容以代码和已核验的官方资料为准；文末保留发布记录。客户端打包及发布证据由 MiloCode 仓库单独记录；本轮未进行付费模型联调。

## 范围与数据边界

桌面客户端为 MiloCode。sub2api 继续负责用户身份、Agent 专用 Key、分组授权、路由和计费；能力档案负责声明模型规格与当前 Responses 执行链能表达的能力。新增档案不会自动添加管理员模型，不会探测上游，也不会改变公开路由 ID、计费、钱包或厂商协议转换。

内置 37 项档案由 desktop_agent_profiles.go 与 desktop_agent_vendor_profiles.go 生成。远程目录可以补充或修正这些数据，经过验证后与内置及已接受目录合并。管理页、配置读写与 bootstrap 使用同一份已接受目录。已保存的管理员能力声明不是官方规格来源。

官方 ID 和文档明确列出的别名采用精确匹配，不按前缀、命名空间或供应商后缀猜测。自定义公开 ID 必须显式绑定官方档案，绑定不改名、不追溯私有上游型号。例如供应商自定义的 deepseek-v4.1-flash 可以由管理员选择对应档案，但它不是内置官方别名，名称本身不能证明实际规格。

## 官方数值合同

每项档案包含：

| 字段 | 含义 |
| --- | --- |
| official_context_window | 厂商明确公布的总上下文；未单独公布时为 null |
| max_input_tokens | 厂商明确公布的独立最大输入；未单独公布时为 null |
| official_max_output_tokens | 厂商明确公布的独立最大输出；未公布或没有独立上限时为 null |
| model.context_window | 厂商总上下文；若只公布独立输入，则使用该输入上限，不将其冒称总上下文 |
| model.max_output_tokens | 已公布最大输出的原始数值；无限或未公布时为 0，并由 output_limit_kind 区分 |
| model.default_max_output_tokens | 管理员设置的正数单次请求预算，和官方最大输出分开 |
| official_reasoning_levels | 官方支持的离散推理档位，仅作官方元数据 |
| model.reasoning_levels | 当前执行链可保真发送的档位，必须非空 |
| notes | 固定说明码；前端只显示已翻译的白名单 |

不使用“上下文减最大输出”、接口默认值、上下文减一或其他推算结果填充官方字段。null 表示未单列，不能显示为 0 Token。所有厂商的运行上下文都不预扣物理最大输出；每次调用仍受既有剩余上下文和请求预算检查约束。

output_limit_kind 规则：

| 值 | max_output_tokens | 运行语义 |
| --- | --- | --- |
| 缺省、空串或 fixed | 正数，允许等于 context_window | 已公布的有界最大输出 |
| unlimited | 0 | 厂商没有独立文本输出上限，仍受上下文和运营预算约束 |
| not_published | 0 | 厂商未公布独立最大输出，不发明数字上限 |

unknown 与 unlimited 的 0 只是线协议标记，不是发送给模型的请求预算。正数运营预算不得超过已公布的最大输出；没有独立输出上限时不得超过上下文。

## 37 项官方核验表

下表中“—”表示该字段未单独公布，对应 JSON null。Claude 的 1M / 200K、128K / 64K 按本轮已核验档案记录为 1,000,000 / 200,000、128,000 / 64,000。Gemini 使用其模型页公布的精确数值。档位表示官方接口能力，不承诺当前统一执行链能发送全部档位。

| 档案及官方来源 | 总上下文 | 独立最大输入 | 最大输出 | 官方推理档位 |
| --- | ---: | ---: | ---: | --- |
| [GPT-6.1 Sol](https://developers.openai.com/api/docs/models/gpt-6.1-sol) | 1,050,000 | — | 128,000 | low / medium / high / xhigh / max |
| [GPT-6 Astra](https://developers.openai.com/api/docs/models/gpt-6-astra) | 1,050,000 | — | 128,000 | low / medium / high / xhigh / max |
| [GPT-6 Sol](https://developers.openai.com/api/docs/models/gpt-6-sol) | 1,050,000 | — | 128,000 | none / low / medium / high / xhigh / max |
| [GPT-6 Luna](https://developers.openai.com/api/docs/models/gpt-6-luna) | 1,050,000 | — | 128,000 | none / low / medium / high / xhigh / max |
| [GPT-5.6 Sol](https://developers.openai.com/api/docs/models/gpt-5.6-sol) | 1,050,000 | — | 128,000 | none / low / medium / high / xhigh / max |
| [GPT-5.6 Terra](https://developers.openai.com/api/docs/models/gpt-5.6-terra) | 1,050,000 | — | 128,000 | none / low / medium / high / xhigh / max |
| [GPT-5.6 Luna](https://developers.openai.com/api/docs/models/gpt-5.6-luna) | 1,050,000 | — | 128,000 | none / low / medium / high / xhigh / max |
| [DeepSeek Flash](https://api-docs.deepseek.com/api/list-models/) | 1,048,576 | — | 393,216 | none / low / high / max |
| [DeepSeek V4 Pro](https://api-docs.deepseek.com/api/list-models/) | 1,048,576 | — | 393,216 | none / low / high / max |
| [Claude Fable 5.2](https://platform.claude.com/docs/en/models/fable-5-2/overview) | 1,000,000 | — | 128,000 | low / medium / high / xhigh / max |
| [Claude Opus 5.5](https://platform.claude.com/docs/en/models/opus-5-5/overview) | 1,000,000 | — | 128,000 | low / medium / high / xhigh / max |
| [Claude Sonnet 5.5](https://platform.claude.com/docs/en/models/sonnet-5-5/overview) | 1,000,000 | — | 128,000 | low / medium / high / xhigh / max |
| [Claude Haiku 5.5](https://platform.claude.com/docs/en/models/haiku-5-5/overview) | 1,000,000 | — | 128,000 | low / medium / high / xhigh / max |
| [Claude Opus 4.6](https://platform.claude.com/docs/en/models/opus-4-6/overview) | 1,000,000 | — | 128,000 | low / medium / high / max |
| [Claude Sonnet 4.6](https://platform.claude.com/docs/en/models/sonnet-4-6/overview) | 1,000,000 | — | 128,000 | low / medium / high / max |
| [Claude Opus 4.5](https://platform.claude.com/docs/en/models/opus-4-5/overview) | 200,000 | — | 64,000 | low / medium / high |
| [Claude Sonnet 4.5](https://platform.claude.com/docs/en/models/sonnet-4-5/overview) | 200,000 | — | 64,000 | thinking 预算，无离散 effort |
| [Claude Haiku 4.5](https://platform.claude.com/docs/en/models/haiku-4-5/overview) | 200,000 | — | 64,000 | thinking 预算，无离散 effort |
| [Grok 4.7](https://docs.x.ai/developers/models/grok-4.7.md) | 500,000 | — | 无独立上限 | low / medium / high / xhigh |
| [Grok 4.6](https://docs.x.ai/developers/models/grok-4.6.md) | 500,000 | — | 无独立上限 | low / medium / high / xhigh |
| [Grok 4.5](https://docs.x.ai/developers/models/grok-4.5.md) | 500,000 | — | — | low / medium / high / xhigh |
| [Grok 4.3](https://docs.x.ai/developers/models/grok-4.3.md) | 1,000,000 | — | — | none / low / medium / high / xhigh |
| [Grok Build 0.1](https://docs.x.ai/developers/models/grok-build-0.1.md) | 256,000 | — | — | 未列离散 effort |
| [Grok 4.20 Reasoning](https://docs.x.ai/developers/models/grok-4.20-0309-reasoning.md) | 1,000,000 | — | — | 未列离散 effort |
| [Grok 4.20 Non-Reasoning](https://docs.x.ai/developers/models/grok-4.20-0309-non-reasoning.md) | 1,000,000 | — | — | 未列离散 effort |
| [Gemini 4 Carbon](https://aistudio.google.com/docs/models/gemini-4-carbon) | 1,048,576 | — | 1,048,576，需 long decode | low / medium / high |
| [Gemini 4 Argon](https://aistudio.google.com/docs/models/gemini-4-argon) | 1,048,576 | — | 1,048,576，需 long decode | low / medium / high |
| [Gemini 4 Hydrogen](https://aistudio.google.com/docs/models/gemini-4-hydrogen) | 1,048,576 | — | 1,048,576，需 long decode | low / medium / high / xhigh |
| [Gemini 3.8 Flash](https://aistudio.google.com/docs/models/gemini-3.8-flash) | — | 1,048,576 | 65,536 | low / medium / high |
| [Gemini 3.6 Flash](https://aistudio.google.com/docs/models/gemini-3.6-flash) | — | 1,048,576 | 65,536 | minimal / low / medium / high |
| [Gemini 3.5 Flash-Lite](https://aistudio.google.com/docs/models/gemini-3.5-flash-lite) | — | 1,048,576 | 65,536 | minimal / low / medium / high |
| [Gemini 3.1 Flash-Lite](https://aistudio.google.com/docs/models/gemini-3.1-flash-lite) | — | 1,048,576 | 65,536 | minimal / low / medium / high |
| [Gemini 3.1 Pro Preview](https://aistudio.google.com/docs/models/gemini-3.1-pro-preview) | — | 1,048,576 | 65,536 | low / medium / high |
| [Gemini 3 Flash Preview](https://aistudio.google.com/docs/models/gemini-3-flash-preview) | — | 1,048,576 | 65,536 | minimal / low / medium / high |
| [Gemini 2.5 Pro](https://aistudio.google.com/docs/models/gemini-2.5-pro) | — | 1,048,576 | 65,536 | low / medium / high（Interactions） |
| [Gemini 2.5 Flash](https://aistudio.google.com/docs/models/gemini-2.5-flash) | — | 1,048,576 | 65,536 | low / medium / high（Interactions） |
| [Gemini 2.5 Flash-Lite](https://aistudio.google.com/docs/models/gemini-2.5-flash-lite) | — | 1,048,576 | 65,536 | low / medium / high（Interactions） |

官方别名的完整集合以生成的 JSON 为准。典型例子：gpt-5.6 归属 gpt-5.6-sol；deepseek-v4-flash 和 deepseek-v4-flash-vision-exp 归属 deepseek-flash；三个 Claude 4.5 短名称归属其明确日期版本。不将去掉 preview 的 Gemini 名称或任意供应商后缀创造为官方别名。

### 推理与媒体能力的执行边界

- OpenAI 维持当前 Responses 文本、图片、PDF、工具、流式及 JSON Schema 能力；原生网页搜索关闭，独立浏览器工具不受影响。
- DeepSeek 依据[原生 Responses API](https://api-docs.deepseek.com/api/create-response/)与[模型元数据](https://api-docs.deepseek.com/api/list-models/)配置。Flash 开图片输入与对话中系统更新；Pro 不继承这两个能力。PDF 需要上传取得 file_id，当前内联 PDF 不开放。
- Claude 新系列复用已有 Responses→Messages 桥的 adaptive effort；4.6 / 4.5 当前只提供自动档，不将旧固定 thinking budget 冒充官方 effort。桥保留图片与内联 PDF，但会前置 system 且未完整转换 text.format，因此 JSON Schema 和对话中系统更新关闭。[官方 effort](https://platform.claude.com/docs/en/build-with-claude/effort)与[上下文说明](https://platform.claude.com/docs/en/build-with-claude/context-windows)是官方元数据来源。
- Grok 4.5 的[官方推理指南](https://docs.x.ai/developers/model-capabilities/text/reasoning.md)将 xhigh 等同 high，当前仅提供可区分的 low / medium / high。4.3 不开放显式 none；Build 与 4.20 使用自动档。[Responses 参数默认输出](https://docs.x.ai/developers/rest-api-reference/inference/responses.md)不充当模型最大值。支持图片、工具、流式、JSON Schema；当前内联 PDF 不开放。
- Gemini 官方档位依据[最新 thinking 表](https://aistudio.google.com/docs/thinking#thinking-levels)。2.5 的新 Interactions 档位与旧 generateContent / OpenAI 兼容层的 budget 映射须区分。当前桌面 Responses 链只提供自动档，开放文本、图片、工具与流式；不因原生多模态规格而自动打开音频、视频、PDF、JSON Schema 或原生搜索。
- Gemini4 的 1,048,576 是[附带长输出条件的官方最大输出](https://aistudio.google.com/docs/long-decode#output-limits)。普通同步输出默认 65,536；完整长输出需要后台执行或 continuation，且受共享上下文剩余空间约束。本轮只准确记录最大值，没有给当前 Responses 执行器新增 Google 原生 Interactions、后台任务或续写协议，不能宣传当前同步链已支持百万输出。

none 继续表示省略 effort、采用上游默认，不能冒充显式关闭思考。官方元数据可包含 none，执行档位则按现有编码能力筛选。无离散官方档位的元数据可为空；执行档位必须非空。

运营默认仍为每次 16,384 Token，属于本项目默认预算，不是厂商最大值或厂商推荐值。OpenAI 默认 medium，DeepSeek 默认 high，其余按档案使用相应默认或自动。读取旧配置时保留公开 ID、展示名及合法的已存推理/输出默认；合法的旧自定义默认不会被强制重置为 16K。

### 生命周期

档案不保证厂商继续开放模型。[Claude 生命周期](https://platform.claude.com/docs/en/about-claude/model-deprecations)中 Sonnet4.5 已弃用，计划 2026-11-30 退役；“not sooner than”不等同已退役。[Gemini 生命周期](https://aistudio.google.com/docs/deprecations)中 2.5Pro / Flash 已弃用并限制既有用户，2026-12-01 起分别自动路由 3.8Flash / 3.5Flash-Lite；2.5Flash-Lite 未弃用但有既有用户限制。模型名存在于档案中不代表当前分组已配置该厂商或完成真实调用验证。

## 远程目录 schema 1

默认维护地址：

https://raw.githubusercontent.com/baisuipingan/sub2api/model-profiles/desktop-agent-profiles.json

目录是数据，不包含 Key、账户网关、计费分组、远程 CEL 表达式、工具实现或任意可执行映射。官方 source_url 只是来源链接，不是调用地址。

~~~json
{
  "schema_version": 1,
  "revision": "2026-11-14.2",
  "profiles": []
}
~~~

以上是 envelope 结构示意；实际目录 profiles 必须非空。本轮完整 37 项导出位于 output/desktop-agent-profiles-official.json，可在 backend 下运行：

~~~sh
go run ./cmd/export-desktop-agent-profiles -output ../output/desktop-agent-profiles-official.json
~~~

运行规则：

1. 内置档案作为启动基线；已接受远程目录与源地址保存在既有 settings 的 desktop_agent_profile_catalog_v1，不新增表或数据库迁移。
2. 启动尝试刷新，随后每 10 分钟刷新；管理员也可以手动刷新。网络请求不进入用户推理热路径。
3. 拉取只接受经过校验的 HTTPS，阻止私网与重定向；启用系统 URL allowlist 时沿用其 PricingHosts。响应上限 2 MiB、超时 20 秒。每个档案的官方来源域名还须匹配对应厂商白名单。
4. 严格校验 schema、唯一 ID / 别名、可空官方数值、执行数值与能力布尔值；未知 schema 或非法内容整批拒绝。
5. 合并保留旧目录中未出现的模型与别名；旧别名不能转移给其他档案，现有档案不能改厂商来源。远程遗漏不能静默删除管理员正在使用的身份。
6. 发布前以候选目录重新校验已存模型、绑定、合法运营默认及分组策略。不兼容变更整批拒绝，不能只发布一半；高级覆盖仍要求显式 override。
7. 持久化先通过 settings CAS 比较旧值，成功后才替换内存目录。并发修改、校验、网络或存储失败保留最后一次已接受的目录并记录安全错误码。内置加最后有效目录支持重启恢复；无远程有效记录时使用内置档案。已保存的安全源地址即使不再被当前 allowlist 放行，也不阻止读取最后有效目录或替换源；实际出站仍按当前策略校验。
8. 管理员保存与目录发布共用更新锁；数据库事务按 catalog→config 顺序加锁，目录提交校验预检时的配置原文，配置提交校验请求采用的目录原文，阻止多实例过期写。preflight 只读一次配置，并在同份原文和固定旧目录上比较候选。持久化不改变可见内存状态；提交成功后在同一临界区发布目录、状态和 CAS 原文。每次配置读取、保存和 bootstrap 固定一个目录 snapshot，嵌套读取复用同一份，避免同一请求混用版本。
9. 保存源地址只修改目录源，不立即替换模型草稿或配置。空地址暂停后续远程拉取并保留有效目录；非空地址可等待周期刷新或点击手动刷新。

### 管理接口

以下路径均位于 /api/v1 下，沿用管理员权限。变更接口使用现有 step-up 保护；不向普通用户开放。

| 方法与路径 | 行为 |
| --- | --- |
| GET /admin/desktop/profiles | 返回当前有效合并目录，形状为 { profiles: [...] }，不返回已存自定义能力模板 |
| GET /admin/desktop/profile-catalog | 返回源地址、版本、数量、最近成功/检查时间、最后错误及是否使用内置基线 |
| POST /admin/desktop/profile-catalog/refresh | 空请求体；完整刷新成功后返回状态，失败保留目录 |
| PUT /admin/desktop/profile-catalog/source | 仅接受 { "source_url": "https://..." }；空串可暂停拉取 |
| GET /admin/desktop/config | 已存配置经过当前目录投影，不自动写回数据库 |
| PUT /admin/desktop/config | 同一目录 snapshot 下解析、验证和保存完整草稿 |
| GET /admin/desktop/models?group_id=... | 读取分组可路由公开模型，不做付费上游探测 |
| POST /admin/desktop/config/disable | 只原子关闭已存开关，不把未保存草稿提交上去 |

状态中的错误码只描述更新失败，不显示未经信任的远程原文或秘密材料。更新目录与更换源地址不自动改变管理员已选的公开模型清单。

## bootstrap protocol 4 与旧客户端

POST /api/v1/desktop/bootstrap 的 protocol_version 新增 4。v4 保留 v3 的账号授权 scope、逐模型不可用原因、空 models 和可空默认选择；只扩展输出上限的表达。ready 响应仍使用现有 provider、models、selection_policy、credential，官方来源元数据不会进入 bootstrap。

v4 允许有界最大输出等于上下文；unlimited / not_published 允许 0 标记，同时要求有效正数运营预算。MiloCode 将 model.context_window 原值传入配置，把 default_max_output_tokens 投影为现有 optionSpecs 的请求预算。它不把 0、接口默认值或 context-1 伪装成物理最大输出，也不增加第二套模型执行器。

旧协议保持严格数值合同：

- v1 / v2 不接收新增字段、0 输出或等于上下文的最大输出；配置包含不兼容模型时整体返回未配置，不发送虚假的兼容上限。
- v3 对不兼容模型使用已有 model_unavailable 原因逐项隔离，其余模型继续可用；若默认模型被隔离，default 为 null，不自动换成别的模型。
- v4 发布完整的新语义。需要使用 Grok 未公布/无限输出档案或 Gemini4 最大输出等于上下文的档案时，需要支持 v4 的新版 MiloCode。

新版 Host 优先请求 v4。只对已知旧服务的固定 HTTP 400 / DESKTOP_AGENT_INVALID 错误形状有界回退一次 v3；旧服务没有专用的 unsupported-protocol 枚举，因此这是固定 bootstrap 请求上的兼容识别。401、403、429、503、网络/超时和非法成功响应不触发协议降级。回退继续走原账户、认证刷新与 epoch fence，不跨账号复用凭据。

Key 仍只存在于 Host 私有材料中；不进入公共模型 Registry、RPC 快照、日志、提示词或持久任务事件。bootstrap 领取前后复查用户、分组、Key 用途与归属；目录更新不改变凭据授权 scope，不取消已经开始的流或工具运行。模型被删除或参数变更时，后续调用沿原有模型成员/配置指纹检查拒绝。

## 管理草稿、覆盖与回退

models 与 model_profiles 属于同一管理员草稿，选择档案或移除模型时一起更新。无 override 时由可信档案生成能力；合法运营默认保留。开启高级覆盖才保留管理员声明；关闭覆盖恢复档案能力并保留合法默认，“恢复档案默认”则同时重置这两个运营默认。

档案加载或校验失败时阻止发布，保留编辑内容。刷新目录不能偷偷提交草稿，重试不能重新加载已存配置覆盖编辑。远程新增档案也不自动加入已存模型。

仍使用既有 desktop_agent_config_v1 设置及新增的目录缓存设置。没有数据库迁移，但旧二进制可能不认识 output_limit_kind 或新档案，不能把“无迁移”理解为任意版本无条件回退。发布与回退应保留设置快照，按现有流程处理配置兼容，不能为模型设置回退覆盖后续余额与使用记录。

## 已核验的本地交付物

本轮已从实际 Go 注册表导出 schema 1 / revision 2026-11-14.2 的 37 项 JSON，包含 7 OpenAI、2 DeepSeek、9 Claude、7 Grok、12 Gemini。新增数据回归覆盖所有 37 项官方原值、null 语义、引用隔离、自定义显式绑定、合法默认保留和协议形状。

- 前端相关测试 149/149，包含档案校验、设置草稿、同步控件、管理 API 和中英文键完整性；vue-tsc -b、改动文件 ESLint、Vite 生产构建通过。
- 后端 service/handler/routes/server 定向单测与真实 PostgreSQL 集成通过；集成覆盖目录首次写入、CAS 以及目录/配置双向并发过期提交拒绝，无跳过。
- 最终并发修正源码的远程目录回归，普通与 -race 均 19/19（11 个顶层测试），无跳过或数据竞争；覆盖原子发布、持久化失败、并发读取及 preflight 单次读取。最终日志位于 output/desktop-agent-catalog-final-{unit,race}.log。此前 race 编译因磁盘不足失败的尝试不算通过；定点清理约 2.38 GiB 旧项目编译缓存后，完整重跑通过。
- MiloCode 本地 Host/账户/准入/交付回归 65/65；canonical 输出类型调整后的 MV04 定向 5/5，完整 typecheck 通过，改动文件 lint 与架构检查通过。
- 浏览器使用实际 Vue 组件和真实导出的 37 项档案，管理同步接口使用本地 mock。验证 DeepSeek 完整 1M 上下文、Grok 无限/未知输出、Gemini4 长输出条件、Gemini2.5 独立输入标签、远程刷新不覆盖草稿；中英文 390px 暗色页面无横向溢出或原始翻译键。证据为 output/playwright/agent-profiles-official-max-qa.json 及同名前缀截图。本验收不代表各厂商已付费联调。

远程 JSON 已实际发布到独立 model-profiles 分支，提交 15465ab2cee2575bd04ef87b0d2080192437d21c。分支仅含 JSON 与维护 README，没有服务发布工作流。稳定地址真实 HTTPS 返回 200，37 项数据与本地审核文件逐字节一致，SHA256 为 f03fd79e44a8959dd93f60a890a51a0dd93aeec22db103cfb12ea135b0ed85a0。核验记录为 output/desktop-agent-profiles-remote-verification.json。后续维护该分支 JSON 即可；服务首次上线本轮读取能力后，无需随每次数值更新重新发版。

## 历史记录：0.2.21-ds.17

以下仅记录 2026-11-14 已发生的历史发布，不能作为本合同 v4 / 远程目录已上线的证据。ds.17 当时含预留输出后的保守上下文/输入预算及部分默认输出 cap；本合同已修正这些表达，不再把它们作为官方最大值。

- 历史版本 fork-v0.2.21-17 / 0.2.21-ds.17，源码 b18f2467cedf9a3b3669cef5b770c3b8111d2485；[当时的完整 CI](https://github.com/baisuipingan/sub2api/actions/runs/43545302576)通过并已部署。
- 历史镜像为 ghcr.io/baisuipingan/sub2api@sha256:e51202de9d6b0cc6faeb4717fd63140f0acf01cdde67322b3e17dd871f9bbc2c。当时应用、PostgreSQL、Redis 与 /health 正常，schema epoch 为 4。
- 当时发布前数据库备份为 /root/sub2api-deploy/releases/backups/sub2api-20261114T023725Z.dump.gz；Agent 设置快照为 /root/sub2api-deploy/releases/backups/desktop_agent_config_vendor_profiles_20261114T021445Z.json。
- 当时已授权的只读管理接口和页面验收确认 37 项旧修订档案可读、DeepSeek 草稿档位可选，既有设置未被测试草稿覆盖；没有付费推理。历史证据位于 output/deploy/fork-v0.2.21-17/。

## 当前发布状态

远程数据分支与服务均已上线。2026-11-14 发布 fork-v0.2.21-18 / 0.2.21-ds.18，源码 4c8ea12d77b7ce206945eb3eec9811cb5d175781；[完整发布 CI](https://github.com/baisuipingan/sub2api/actions/runs/43561614368) 的后端单测、数据库集成、Lint、前端测试及构建全部通过。

生产按 ghcr.io/baisuipingan/sub2api@sha256:6ca949e5fc02263f011e4cd7f1b6072fcabde916a82a2639036691cc79dc1cdc 部署，schema epoch 仍为 4。部署前数据库备份为 /root/sub2api-deploy/releases/backups/sub2api-20261114T055337Z.dump.gz，Agent 原文快照为 /root/sub2api-deploy/releases/backups/desktop_agent_catalog_pre_ds18_20261114T052420Z.json。

发布后验收确认应用、PostgreSQL、Redis 与公开健康检查正常；远程 37 项档案与审核 JSON 完整一致，revision 为 2026-11-14.2，using_fallback=false，最近刷新成功且无错误。已保存 Agent 配置原文与模型列表保持不变，顶部下载入口、Mac 辅助脚本及公开下载接口均正常。证据位于 output/deploy/fork-v0.2.21-18/。未进行各厂商付费推理、长 decode、工具或计费联调。
