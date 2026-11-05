# 生图工作台：厂商参数与协议

核对日期：2026-11-05。模型列表仍只使用目录实际返回的公开 ID；能力模板不会添加、替换或改名可选模型。

## 识别与参数切换

工作台目录保留 `id`、`api`，增加 `profiles`。后端沿组合路由、渠道映射、账号映射解析最终模型，只返回固定的厂商/能力族枚举，不返回私有上游型号、账号或凭据。`api=openai` 表示 Images 请求协议，不等于模型来自 OpenAI。

```json
{"id":"my-banana","api":"openai","profiles":[{"provider":"google","profile":"gemini-3.1-flash-image"}]}
```

同公开 ID 有多种承接能力时，界面取共同支持项。订阅账号的 Images 桥接增加 `transport:bridge` 约束，保留既有参数，但不承诺新 Images 字段。未知映射保持未知，不能用公开别名覆盖后端结论。旧接口没有元数据时，前端才匹配已知家族及其合法版本边界，不把 `2.60` 当 `2.6`。

## 已接入参数

| 厂商/能力族 | 清晰度与比例 | 其他主要参数 | 请求 |
| --- | --- | --- | --- |
| GPT Image 2 | 自动/1K/2K/4K、像素尺寸，遵守官方边长和像素约束 | 质量 auto/low/medium/high，PNG/JPEG/WebP、压缩、参考图、遮罩、透明背景 | Images JSON 生成、multipart 编辑 |
| GPT Image 2.5 | 同上 | 增加 xhigh/max；渐进图片按现有流式契约处理 | 同上 |
| GPT Image 2.6 Flare/Sunburst | 同上 | 质量与规划复杂度独立；complexity=low/medium/high，默认省略；保留栅格输出参数 | 同上 |
| Gemini | 按具体型号展示 512/1K/2K/4K 和允许的比例 | 参考图；不发送 GPT 质量/压缩/遮罩参数 | 原生 generateContent 的 imageConfig 与 inlineData |
| Grok Imagine Image 2.0 | 默认（显式发送官方默认 1k）/1K/2K、官方比例 | quality=auto/low/medium，单请求 n=1–10，最多五张编辑输入；legacy 保留三张上限 | Images JSON，resolution/aspect_ratio，image 与 images 互斥 |

Google 经第三方 Images 接口时继续使用该协议，不假设原生 `imageConfig` 一定生效；不开放兼容协议未经保证的 512 和超宽比例。逐型号差异见 [Gemini 参数核对](GEMINI_IMAGE_PARAMETERS.md)。

切换模型会清除不支持的遗留参数。原生比例单独保存，不经过 GPT 最大 3:1 的像素约束，因此 Gemini 1:8 不会变成别的比例。Grok 使用专用 JSON 编辑适配。

## 兼容范围与官方文档差异

- OpenAI 2.6 文档另有 SVG、动画 GIF、markup、prompt_rewriter/SVG 作者选择。本工作台没有这些功能对应的文件编辑器、动画预算和导入导出契约，因此不在当前栅格面板中开放。GIF 帧数上限在 guide（64）与 API reference（16）有差异，不能据此猜测权限。
- OpenAI 2.6 的质量只开放已明确确认的 auto/low/medium/high，不从 2.5 自动继承 xhigh/max。GPT Image 2 的透明背景在官方 reference 中仍标记为 preview；保留已有能力，不保证供应商开通。
- Grok reference 列出 1.5k，但生成 guide 与网关现实现仅确认 1k/2k。本次不开放 1.5k、4K、deferred/polling。旧 quality slug 的迁移不等于所有 Grok 型号都支持 2.0 参数。
- Gemini 继续使用现有 generateContent，不切换到新 Interactions API，不声称保存原生多轮 thought-signature 会话。thought=true 中间图片不作为最终结果计数。
- 原生 512 归入现有 1K 账单档，不新增数据库计价档。Grok resolution 补充内部尺寸统计，原始上游请求不注入 GPT size。

## 官方来源

OpenAI：

- [Image generation guide](https://developers.openai.com/api/docs/guides/image-generation)
- [Images create reference](https://developers.openai.com/api/reference/resources/images/methods/generate)
- [Images edit reference](https://developers.openai.com/api/reference/resources/images/methods/edit)
- [GPT Image 2.6 Flare](https://developers.openai.com/api/docs/models/gpt-image-2.6-flare)
- [GPT Image 2.6 Sunburst](https://developers.openai.com/api/docs/models/gpt-image-2.6-sunburst)

Google：

- [Image generation guide](https://ai.google.dev/gemini-api/docs/image-generation)（目前重定向到官方 AI Studio 文档）
- [REST ImageConfig](https://ai.google.dev/api/generate-content#ImageConfig)
- 逐模型来源见 Gemini 参数核对文档。

xAI：

- [Generation](https://docs.x.ai/developers/model-capabilities/images/generation)
- [Editing](https://docs.x.ai/developers/model-capabilities/images/editing)
- [Multi-image editing](https://docs.x.ai/developers/model-capabilities/images/multi-image-editing)
- [Images reference](https://docs.x.ai/developers/rest-api-reference/inference/images)
- [Quality model migration](https://docs.x.ai/developers/migration/imagine-image-quality-nov-2)

完整原文、提取文本和获取记录保存在本工作区 `output/docs/image-providers/`。资料不包含 API 密钥，核对文档及回归没有调用收费生图接口。
