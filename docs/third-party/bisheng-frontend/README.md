# Bisheng 工作台 Vue 移植记录

固定来源：`dataelement/bisheng@a8e5be89c1b8d3a2e512c772e2a38de409336dc8`（2026-11-17，Hotfix/3.0.2）。源路径和 SHA-256 见 `source-manifest.json`。许可证与归属随本目录保留；此处是修改过的 Vue 适配，不是原版 React 应用。

## 保留的结构和行为

- 工作台共用单条侧栏；模块入口下方是平铺会话历史，不增加第二条常驻侧栏。
- 新对话居中的欢迎区和输入框；已有会话使用底部固定输入。模型选择保留在输入框右侧。
- `＋` 菜单中的任务模式和联网搜索；已启用能力显示可关闭标记。不显示后端尚未启用的操作。
- 用户气泡、无卡片边框的回答、手动上滚解除自动跟随、回到底部、消息复制及重新生成。
- 消息内真实执行步骤、排队、澄清卡；输入框上方可折叠的任务清单。任务未真正结束时不宣布全部完成。
- 单个成果的交付链接与下载；多个成果的完整文件列表。
- 桌面右停靠 `clamp(440px,46%,720px)` 文件面板、平板右抽屉、手机全屏。停靠和全屏只挂载一个预览实例。
- 澄清一页一问，单选即前进、多选确认，自填、跳过及提交已收集回答。
- 内容和输入宽度 `clamp(680px,64cqw,920px)`；系统字体、14px 正文、28px 输入框圆角和轻描边/阴影。样式同时依据原 TSX 和最终 CSS 覆盖。

## 明确的集成差异

- 本站保留工作区切换、220px/58px 主侧栏与全站顶部栏；聊天占用其下无内边距内容区。没有引入 Bisheng 的登录、模型管理或余额系统。
- 使用本站 `Select`、`TextArea`、`ConfirmDialog`、图标。任务/文件特有组件保持原结构，所有样式仅作用于 `.bs-chat` 及专用门户，不导入原全局 CSS。
- 排除工作流、RAG、知识空间、应用广场、项目及用户自上传技能菜单。原侧栏项目有 mock 存储，实现中没有复制。
- 本站模型以公开模型 ID 和已核实能力决定工具与推理强度；不复制 Bisheng 的模型注册表。输入区增加本站模型已支持的推理强度选择。
- 前后端事件和权限统一使用本站接口，SSE 只订阅持久事件；断线重连不重发付费请求。流式回答来自真实 `assistant.delta`；最终以持久消息为准。
- 成果继续修改使用 `target_resource_version_id` 指定不可变文件版本；不会仅把文件名写入提示词。
- 原件/预览访问经当前用户鉴权返回短期链接，签名与模型 Key 不进入浏览器持久会话。文件列表采用元数据分页；只有打开文件时才申请链接/拉取内容。
- 普通问答不依赖文件执行槽。执行步骤只展示服务端允许的用户文案，不展示脚本、日志或模型内部推理。

## 交互与安全验证

已有针对 SSE 分帧、取消后步骤状态、成果去重、澄清卡、多页历史、登出时在途请求、未知请求重用同一 run ID 的自动测试。前端 `vue-tsc --noEmit` 已执行。

真实链路和视觉验收必须在目标服务可运行后执行：真实普通问答/停止/刷新恢复、文档生成/预览/下载/修改版本、两用户越权、上传限额、断线恢复、移动端抽屉和暗色。自动测试使用 fixture 验证组件行为，不充当线上 API 的替身。

本次未运行完整 Bisheng 服务，不声称像素一致。后续升级应先检查源 manifest 对应活跃引用链，避免只凭组件名搬到原项目已弃用的 `PlusMenu`/`TaskModeToggle` 路径。


## Source component baseline

A separate research-only React harness mounts the unmodified upstream `Landing`,
`ClarifyCard`, and `ResultSection`, together with upstream `clarify.ts`, chat CSS,
and the shared Tailwind preset at the pinned commit. API/config/localization,
icon, and unsupported viewer boundaries have small test adapters. This is not the
complete Bisheng application and is not shipped in the Vue production bundle.

On 2026-11-19 the harness was opened in a real Chromium browser at 1440×1000 and
390×844. Single-choice advance, multiple-choice explicit confirmation, exact
answer composition, 26px heading and 16px clarification-card radius were checked.
Evidence is in `output/research/bisheng-office/source-baseline/` (ignored research
artifacts). This baseline supports those specific component/layout decisions;
it does not assert pixel equality for all screens or test the upstream backend.

All 9 source-to-target mapping groups in `source-manifest.json` were re-hashed
against the downloaded pinned sources with zero mismatches on 2026-11-19.
