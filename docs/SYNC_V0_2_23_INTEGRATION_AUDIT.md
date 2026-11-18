# 官方 v0.2.23 与定制链路整合审计

日期：2026-11-18。合并分支：`codex/sync-v0.2.23`；合并前提交：`ee82f6b23`；官方标签：`v0.2.23`，提交 `5af9afb588c4b014b3df9db7d4073af1f8ae01b6`。

## 合并方式与范围

从上次已验收的 v0.2.22 定制分支创建新分支，对官方发布标签执行完整 `git merge --no-ff`，没有挑拣提交或用官方文件整体覆盖定制功能。官方新增 20 个非 merge 提交、变动 90 个文件。合并没有文本冲突；4 个与既有定制内容重叠的文件另行逐项审查。

- 86 个上游改动文件保留 v0.2.23 原始内容。
- `gateway_usage_billing.go` 在保留积分命令/流水快照的基础上，接入官方 `billingUsageTokens`，让账号成本和用户售价都得到相同的缓存 TTL 明细。
- 两份 `admin/overview.ts` 保留 fork 统计文案，同时完整更新官方 Ultra Fast 套餐、促销截止与倍率说明。
- `backend/cmd/server/VERSION` 使用 `0.2.23`，遵守 fork 发布流程要求。官方发布标签中的该文件仍为 `0.2.22`，不能直接沿用来生成 fork 版本。

## 官方更新在定制系统中的实际效果

| 官方更新 | 定制接入与验证 |
| --- | --- |
| GPT-6.2 Sol/Astra、长上下文及 Fast/Ultra Fast 定价 | 共享模型、计价、参数转换与 OAuth 套餐门控直接生效；两套 RecordUsage 传递请求时点，促销切点不会按结算时刻漂移 |
| 渠道 Ultra Fast 倍率 | 管理表单、复制/保存、handler、service、repo、resolver 全链使用 nullable 字段；不填沿用模型默认，正数覆盖不污染 Fast/Flex |
| 缓存 TTL 与差额计费 | Anthropic、Responses、Chat Completions 互转保留汇总与 5m/1h；未知差额按 5m，已知 1h 保留；最终成本统一进入 credits/shadow/legacy，账号成本同步 |
| cache-only 流式用量更新 | 共享 usage snapshot 归一修复先形成互斥用量桶，再进入售价与积分快照，不为桌面/工作台保留旧解析器 |
| 首输出前 idle 换号 | 继承官方一次切换预算；仅 keepalive 不被误判正文，已输出内容后不重放第二个账号的答案 |
| 减少原生 CC flush、DeepSeek/Ollama 单次 JSON 改写 | 保留官方实现，兼容 fork 会话头、模型名还原、真实上游模型观察和组合分组 |
| 安全依赖补丁 | 保留 follow-redirects 锁定/覆盖，继续使用项目固定 pnpm 主版本及冻结锁文件 |

本次没有改写 v0.2.22 已完善的积分聚合 SQL。共享定价/usage 修复先进入美元成本，再生成相同命令的整数积分快照，因此当前 `credits` 模式可取得新修复，无需切换 legacy。

MiloCode 通过受管 Key 调用公共网关，图片工作台通过完整 router 执行请求；均继续共用上述调度、认证与结算。文本 SSE 的 idle/flush 改进不被宣称为非流式生图的时长优化。

## 补齐自动合并不能解决的部分

1. **MiloCode 能力档案**：网关模型目录与桌面官方能力档案是两个有不同职责的注册表。本次为后者补 GPT-6.2 Sol/Astra：官方 API 上下文 1,050,000、单独输入上限 922,000、输出 128,000，effort low 至 max。旧远端 37 项不会删掉新内置项；39 项导出工件已准备，旧 37 项内容未改变。
2. **档案导出**：保留各项真实审核 revision，导出 envelope 取最新 revision，移除所有条目必须同 revision 的旧硬编码约束。
3. **回归断言**：更新 Ultra Fast 旧帮助文案断言及内置档案数量断言，保留权限与配置 payload 的实际行为测试。
4. **积分组合回归**：新增 54 个场景，覆盖 3 个引擎 × 2 个 RecordUsage 入口 × 9 个定价场景，独立常量验证促销前后、渠道覆盖、1h/5m 差额、订阅峰值和钱包/Key/账号额度快照。
5. **共享目录回退漏传策略**：组合测试发现通用 `CalculateTokenCostForRequest` 在没有 resolver/effort 时调用旧三参数入口，丢失请求 tier 和计价时点，导致 GPT-6.2 Ultra Fast 按标准价计算。正常 Wire 装配有 resolver，但这条共享兼容路径仍应正确。已将其统一交给支持 resolver 缺省的 `CalculateCostUnified`，不复制新公式；直接回归同时覆盖 Standard、Fast、Flex、促销切点以及原有无策略参数时的长上下文、负/零倍率和返回字段语义。

## 数据库兼容

官方 305 份 SQL 与标签逐字节一致；合并前已存在的 317 份 SQL 全部保持原样。唯一新增迁移是 `252_channel_pricing_ultrafast_multiplier.sql`，给 `channel_model_pricing` 增加 nullable `NUMERIC(12,6)` 列和正数或 NULL 约束，不回填或改写已有倍率。

迁移身份以完整文件名登记，因此与已有 `252_ticket_read_state.sql` 不冲突。`.fork-release.json` 追加本次说明，仍为 schema epoch 4 / `backup-required`，没有将回退策略放宽成只切镜像。

## 验证记录

详细证据见 [积分计价审计](SYNC_V0_2_23_BILLING_AUDIT.md)、[转发审计](SYNC_V0_2_23_FORWARDING_AUDIT.md)、[界面、迁移与模型档案审计](SYNC_V0_2_23_UI_MIGRATIONS_AUDIT.md)。日志保存在忽略目录 `output/sync-v0.2.23/`。

- 完整后端 integration：`CI=true GOMAXPROCS=2 go test -tags=integration -p 2 ./...` 通过，实际创建临时 PostgreSQL/Redis 并应用全部迁移；不是没有 Docker 时跳过测试。
- 前端全量初次执行 483 文件、4,154 测试：4,153 通过，1 个旧帮助文案断言失败；修正后连同相关配置行为重跑 5 文件、34 测试通过，另新增一条促销说明回归。typecheck、lint 和生产 build 通过。
- 档案 exact ID/自定义别名、协议 1–3 bootstrap、旧远端刷新/重启、真实导出工件测试通过。
- 转发与协议专项通过；第一次完整 unit 中 routes 的旧 37 项断言失败，修正后 routes 整包 unit 补跑通过。
- fork 发布、部署、备份回退、Apple 容器、Compose 安全/环境/资源与 Caddy 缓存检查通过。

完整后端 unit 及提交后的 CI 结果在完成后补记。没有因测试通过而宣称所有供应商都已付费在线联调。

本轮仅合并和验证代码；未创建发布标签、未部署生产、未发布远端能力档案，也未修改 ZCode 仓库或制作客户端包。
