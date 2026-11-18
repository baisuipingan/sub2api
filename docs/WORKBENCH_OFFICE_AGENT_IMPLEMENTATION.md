# 工作台办公 Agent 实施记录

设计依据：`WORKBENCH_OFFICE_AGENT_DESIGN.md`。分支：`codex/workbench-office-agent`。

## 实施顺序

| 步骤 | 可验收交付 | 状态 |
| --- | --- | --- |
| P0-A 持久控制面 | 默认关闭的管理员试验入口；接单/租约/事件/模型 invocation inbox；原网关调度计费；取消与未知结果防重放 | 基础实现；数据库/网关契约测试通过，真实上游计费待验收 |
| P0-B 真实内核移植 | 锁定来源的 Bisheng 技能/脚本/精选逻辑；DeepAgents；本站模型桥；无本地脚本 fallback | 已实现；真实 Python 进程与 Go 模型桥通过，办公调用边界使用 mock 验证 |
| P0-C 隔离办公验证 | 固定依赖镜像；任务容器限制；DOCX 重读与渲染、XLSX 重算、PPTX 渲染探针 | 监督组件与探针已实现；真实 Linux/gVisor 与 Office 渲染尚待验收 |
| P0-D 恢复与归档闭环 | 检查点 fencing；文件版本归档；继续修改；真实网关计费与故障验证 | 待 P0-A/B/C 接通 |
| P1 对话核心 | Vue 页面、文本模型目录、持久会话、SSE 重连、搜索及用量 | 待 P0 关键契约通过 |
| P2 办公与资源库 | 文件上传、任务成果、版本/预览、图库引用 pin、共享存储保护 | 待前置验证 |
| P3 管理与开放 | 管理指标/任务/权限、容量压测、小流量开关、回归与发布 | 待前置验证 |

## 本阶段边界

先通过新增且默认关闭的内部试验 API 跑通真实后端，不修改个人工作台默认路由，不向普通用户展示尚未验收的办公功能。新模块不得启动时扫描/修改旧图片任务、创建另一把工作台 Key、接管积分账本或改变现有存储清理。

每一步都记录自动测试与实际环境验证；mock 测试不作为真实上游计费、Linux 沙箱隔离或 Office 视觉效果已验收的证据。缺少运行环境时保留关闭状态，明确剩余验证项，不静默使用宽权限执行器替代。

## 验证与恢复原则

- 先接纳消息/run，再允许 worker 调模型；请求 ID 注入 Go context。
- operation inbox 独立于账本去重；付费调用已发出但结果未知时不自动重发。
- 可信编排不执行模型生成代码，所有 Python 办公脚本只进固定隔离执行器。
- 新增表与配置不依赖生产启用；关闭试验入口不影响图片、API、桌面端与工单。
- 数据层测试覆盖并发接单、租约过期、取消、重复提交、晚到结果、事件排序和预算。
- 执行器测试覆盖路径/符号链接、网络与凭据边界、超时/资源限制、输出检查和清理。

## 首批实现的实际结构

- `backend/internal/service/office_agent*`：任务、短期租约凭证与模型桥。Go 从现有工作台配置读取分组，复用原用户工作台系统 Key，调用原 `/v1/chat/completions` router，沿用原鉴权、路由及积分链；没有另一套扣费逻辑。
- `backend/internal/repository/office_agent*` 与 `dsapi_006_office_agent_control_plane.sql`：新增六张独立业务表，保存会话、消息、run、outbox、操作 inbox 和事件。旧生图、Key、usage、积分表没有结构修改。新版本启动的自动迁移会新增这些表，即使功能关闭；本轮未部署。
- `services/office-agent/`：锁定 DeepAgents/LangGraph，真实装配 Bisheng 原始中间件及三套完整办公技能。文件来源 commit、上游及本地哈希、修改说明与 Apache 许可证一并纳管。
- `services/office-executor/`：可信监督器与不可信任务镜像；只允许固定摘要、Linux/runsc、无网络/无凭据、按任务限时限资源。主站不持有 Docker socket，普通聊天无需启动该组件。
- `.github/workflows/backend-ci.yml`：新增 Python 契约、执行器测试、Go 并发回归及真实 Python 进程桥接验证。真实 Office/gVisor 验收由独立脚本完成，CI 不拿普通容器代替。

Go 不向 Python 发放原 API Key。运行凭证绑定用户、run、worker、epoch 和截止时间；模型请求不能自行选择另一个公开模型或地址。每次模型/办公操作先持久登记再派发，同一 operation 重复请求只读原结果；结果未知时停止自动重放。取消后只允许原派发身份保存晚到结果，不能重新启动任务。数据库亦限制任务总时长，续租及回收不会重置该时长。

## 内部试验入口

保持默认关闭。仅显式配置以下参数才能开启试验（值为示例，不是生产建议）：

```yaml
office_agent:
  enabled: false
  worker_token: ""  # 启用时必须为独立随机的 32–256 字节可打印 ASCII，无空格
  max_concurrent_calls: 1
  max_model_calls: 8
  max_tool_calls: 16
  max_output_tokens: 8192
  max_run_seconds: 600
```

Go 环境变量对应 `OFFICE_AGENT_ENABLED`、`OFFICE_AGENT_WORKER_TOKEN` 等。Python 使用 `OFFICE_CONTROL_ORIGIN` 与 `OFFICE_WORKER_TOKEN`（值与 Go worker_token 相同，名称不同）。执行器另用独立 token，不能复用此凭证。内部接口必须通过受信私网或加密通道访问，不将执行器端口暴露公网。

管理员试验 API 沿用原 admin 鉴权、审计与面板限流，只能操作自己名下的试验会话：

| 方法与地址（`/api/v1/admin/workbench/office-pilot` 下） | 用途 |
| --- | --- |
| `GET /status` | 默认关闭及本阶段能力状态 |
| `POST /runs` | `run_id`、可选 `conversation_id`、公开 `model`、文本 `content`；稳定 run ID 用于接单幂等 |
| `GET /runs/:id` | 状态、预算与安全错误 |
| `GET /runs/:id/messages?after=0` | 按持久序号读取所属会话正文，每页最多 100 条 |
| `GET /runs/:id/events?after=0` | 按持久序号读取进度事件，每页最多 100 条 |
| `DELETE /runs/:id` | 取消任务并使原 lease 失效 |

worker 领取与续租、模型桥和工具 inbox 位于 `/internal/office-agent/v1`。bootstrap token 只能领取任务；具体操作必须用绑定该 run 的 lease。不会把普通浏览器 JWT 当作 worker 凭证。

## 本轮验证记录（2026-11-18）

- 原 `config/service/handler/server/routes` 整包测试通过，包括桌面端已有模型与配置测试。
- 同一修改范围的办公与桌面端定向 `-race` 测试通过。
- `go build ./cmd/server` 服务端完整构建通过；仅生成临时验收二进制，没有启动主站或执行生产迁移。
- Agent 24 项测试通过，覆盖真实图构建、两轮工具调用、操作身份、无自动重试、未确认终止、工作区边界及来源文件校验。
- 执行器 28 项测试通过，覆盖认证、HTTP、固定容器参数、路径/压缩包/MIME、幂等、配额、满盘拒绝、取消、容器崩溃未知结果及清理失败停接任务；这些测试不执行模型代码。
- 真实子进程探针使用同一 Python runtime，经 HTTP 调 Go 模型桥并完成最终回复；上游回复为 fixture，不能据此声称真实供应商计费已验收。
- PostgreSQL 11 项并发/租约/操作 inbox 测试通过，包含锁等待后过期与总时长不可续租重置；临时真实数据库在测试结束后清理，没有运行中容器残留。
- Docker Compose 静态配置与 workflow YAML 可解析；本机 `doctor` 拒绝 macOS/无 gVisor，没有降级。

## 继续实施与开放条件

接下来先完成 P0-D 的持久 saver fencing、必要工作快照及对象存储版本归档，并对真实供应商成功、取消、断线与计费做验收。文件原件和成果通过 Go 资源权限层流转，不能把当前 RAM workspace 当成云端资源库。现有图库对象的 retain/pin 必须在对话可引用旧图之前接通。

随后推进 P1 原生 Vue「对话助手」页面：会话、模型、输入与停止、断线重连、用量；再接 P2 文件和成果面板、版本与继续修改。待 P3 管理配置、容量与故障验证通过后再开放给普通用户。

当前明确未交付：个人聊天页面、联网搜索、持久图恢复、流式 token、文件云端下载/归档和可恢复提问。未启用试验时没有用户菜单、后台轮询 worker、任务容器或自动生产发布。
