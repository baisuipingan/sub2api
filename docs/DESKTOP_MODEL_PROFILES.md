# 桌面模型官方档案与执行能力

2026-11-13 修正。本合同替代此前“从已保存配置导出能力模板”的设计。旧能力由管理员随填，不能作为可信事实复用。本轮不改变模型网关、钱包、计费或 ZCode 引擎。

## 来源与范围

内置档案位于 `backend/internal/service/desktop_agent_profiles.go`，随代码发布，运行时不联网探测。`GET /api/v1/admin/desktop/profiles` 仅返回内置档案，不再读取已保存模型的能力值。每项包含官方来源 URL、核对时间、修订号、官方规格、当前执行器投影及限制说明。

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

仅精确匹配上述官方 ID，以及官方文档明确列出的 `gpt-5.6` → `gpt-5.6-sol` 别名。不靠前缀猜测，不把 `openai/xxx` 或供应商后缀自动当成官方 ID。自定义公开名可以显式关联档案，关联后仍使用原公开 ID 路由，不回溯供应商私有名。

## 官方能力与实际可用能力

这些档案均支持文本/图片输入、文本输出、流式、工具调用、结构化输出；音频/视频输入保持关闭。PDF 支持依据官方 [File inputs](https://developers.openai.com/api/docs/guides/file-inputs) 与现有 Responses 文件序列化能力；对话中系统消息依据 [Responses 输入消息合同](https://developers.openai.com/api/reference/resources/responses/methods/create) 及现有执行器兼容转换。

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
