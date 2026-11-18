# v0.2.23 渠道定价、界面、迁移与桌面能力档案审计

审查日期：2026-11-18。官方基准为 `v0.2.23`，工作分支为 `codex/sync-v0.2.23`。本记录只覆盖本次独立审查负责的渠道表单/持久化、统计界面、迁移和 MiloCode 模型档案；转发与积分计费链路另有审计记录。

## 结论及补充修正

渠道 Ultra Fast 倍率已完整进入前端创建、编辑、复制、保存、后端校验、数据库读取和两种计价路径。既有未配置的渠道继续继承模型默认倍率。定制的模型使用分布展开用户明细保持完整。

发现并修复了一个 fork 集成遗漏：官方新增 GPT-6.2 Sol/Astra 后，独立的 MiloCode 内置能力档案仍只有此前的七个 OpenAI 模型。网关模型元数据不会自动成为桌面能力档案；档案按精确 ID/显式别名解析，因此需要在原有注册表新增这两个模型。已补充官方最大值、端到端 bootstrap 回归和旧远端档案兼容回归，档案总数由 37 增至 39。

此外，既有前端测试要求 Ultra Fast 帮助文案包含旧技术串 `service_tier=ultrafast`，与官方新增的套餐和优惠说明不再一致。保留官方产品文案，更新测试检查新套餐及优惠日期/倍率语义，并验证原有 Ultra Fast 开关 payload 行为。

## 渠道 Ultra Fast 倍率全链

| 层次 | 核查位置与行为 |
| --- | --- |
| 表单控件 | `PricingEntryCard.vue` 只在 OpenAI 渠道显示 Ultra Fast 倍率；支持空值继承以及正数自定义值，显示模型默认值帮助信息 |
| 创建/编辑/复制 | `ChannelsView.vue` 的空表单、复制、API 回填、保存转换均保留 `ultrafast_multiplier`；空白转为 null，非正数阻止提交 |
| API 与类型 | `frontend/src/api/admin/channels.ts`、渠道表单类型、`channel_handler.go` 请求和响应使用同一 nullable 字段；门控与原有渠道覆盖机制一致 |
| 服务校验 | `channel.go`、`channel_service.go` 要求自定义倍率大于 0；允许 null 继承 |
| 数据库 | `channel_repo_pricing.go` 的查询、批量查询、创建、更新和整批替换均包含新列；scan 顺序一致 |
| 价格解析 | `model_pricing_resolver.go` 传递 `UltrafastMultiplierOverride`，官方 `channel_ultrafast_pricing_test.go` 覆盖两条计价路径的 null/自定义/1倍/小数倍率 |

本次没有为 fork 的 credits 或工作台另建渠道倍率字段。积分扣费仍消费共享计价链形成的实际成本；该部分的组合数值验证见积分专项审计。

## 管理端统计与界面

- 定制 `DistributionBars.vue` 与 `admin/UsageView.vue` 的模型展开用户明细未被上游覆盖，筛选参数传递和 `enableBreakdown` 入口保留；本次官方没有修改这两个实现文件。
- 官方 Ultra Fast 帮助文案保留 GPT-6 Astra / GPT-6.1 Sol，同时增加 GPT-6.2：Pro 200 支持 Sol，Pro 500 支持 Sol 和 Astra。6.2 在太平洋时间 2027-02-28 及之前为标准价 3 倍，之后为 6 倍。
- `groupsOpenAIFast.spec.ts` 的旧文案断言改为检查以上中英文语义。`GroupsView.openaiForcedTier.spec.ts` 同时验证 Fast/Ultra Fast 开关可以独立保存、共同开启和关闭，避免仅更新文案而遗漏行为。

## 迁移与数据兼容

新 SQL `252_channel_pricing_ultrafast_multiplier.sql` 向既有 `channel_model_pricing` 增加 nullable `NUMERIC(12,6)` 列及正数约束。NULL 继承模型默认值，旧记录无需回填固定倍率；DDL 使用 `IF NOT EXISTS`，不删除已有定价记录。

迁移执行器 `backend/internal/repository/migrations_runner.go` 使用完整 filename 作为主键，并按完整文件名排序。新增官方 `252_channel_pricing_ultrafast_multiplier.sql` 与已发布 fork `252_ticket_read_state.sql` 名称不同，可以各自登记执行；未重命名已有迁移。

通过 `git ls-tree` 筛选 `.sql`，再使用 `git cat-file --batch` 取得原始 blob 与工作树逐字节比较：

- 官方 v0.2.23 的 **305 个 SQL 全部字节一致**。
- 合并前 HEAD 的 **317 个 SQL 全部未改**。
- 合并后的 318 个 SQL 中仅新增上述一个官方迁移。

此处计数只包含 SQL，不把同目录 Go 测试计入迁移数量。字节比较比运行时 trim 后的 SHA256 校验更严格。生产备份、实际升级和回滚属于发布流程；本审计未连接生产数据库。

## GPT-6.2 桌面能力档案

官方模型资料：

- [GPT-6.2 Sol](https://developers.openai.com/api/docs/models/gpt-6.2-sol.md)
- [GPT-6.2 Astra](https://developers.openai.com/api/docs/models/gpt-6.2-astra.md)
- [Reasoning effort](https://developers.openai.com/api/docs/guides/reasoning)

本次两项档案均使用官方 API 公布值：

| 字段 | GPT-6.2 Sol / Astra |
| --- | --- |
| 共享上下文最大值 | 1,050,000 tokens |
| 单独公布的最大输入 | 922,000 tokens |
| 最大输出 | 128,000 tokens |
| 普通 API reasoning effort | low / medium / high / xhigh / max |
| 输入 / 输出 | 文本、图片输入；文本输出；不宣称支持音频或视频 |
| 审核日期 / revision | 2026-11-18 / 2026-11-18.1 |

`none` 不适用于这两个模型；没有擅自加入 `minimal`。官方 API reasoning 文档列出模型相关的努力级别并包含 `max`，没有列出 `ultra`。本次上游原生 Codex 元数据还含 `ultra`，那是另一条 Codex 执行能力元数据，未据此扩展现有 MiloCode 普通 API 协议。也没有把 Codex 元数据中的执行窗口/压缩预算冒充为官方 API 最大值。

档案 `OfficialReasoningLevels` 与实际下发 `Model.ReasoningLevels` 均保留 low 至 max；本次没有隐藏 max。最大输出 128,000 与既有默认输出 16,384 分开，默认值不改变官方上限。上下文不做“减去最大输出”的派生扣减。

新增测试 `desktop_agent_gpt62_profiles_test.go` 覆盖：

1. 两个精确模型 ID，以及显式绑定这些档案的企业自定义公开别名。
2. 协议 1、2、3 的真实 bootstrap：保存的旧小窗口不能压低档案公布的最大值，保留 max 努力级别；不把仅管理端使用的档案元数据泄入旧协议。
3. bootstrap 不写回或重置管理员保存的模型配置。
4. 旧远端 37 条档案刷新、缓存及重启以后，新内置两项仍存在，保留官方最大值与新 revision。

旧 37 项档案的内容和各自 revision 保持不变。

## 远端档案与导出工件

默认地址仍为 [model-profiles 分支的 desktop-agent-profiles.json](https://raw.githubusercontent.com/baisuipingan/sub2api/model-profiles/desktop-agent-profiles.json)。审查时该分支 HEAD 为 `15465ab2cee2575bd04ef87b0d2080192437d21c`，远端仍为 37 项。

`mergeDesktopAgentCatalog` 保留远端省略的已有 ID，因此服务升级后的新内置项不会被旧远端 37 项删掉。接受的合并快照保存在设置 `desktop_agent_profile_catalog_v1`，沿用启动、10 分钟及手动刷新流程。旧服务实例必须升级二进制或获得更新的远端档案才能认识这两个新 ID。

原导出工具硬编码要求每项 revision 完全相同，会拒绝“保留旧档案 revision，只新增两条审核记录”的正常输出。本次最小修正为拒绝空 revision，并以最新档案 revision 形成文档 envelope；未改变档案格式或下载协议。

实际运行：

```sh
cd backend
go run ./cmd/export-desktop-agent-profiles -output ../output/sync-v0.2.23/desktop-agent-profiles-official.json
```

输出 schema_version=1，revision=`2026-11-18.1`，39 项。以解析后的 JSON 对比当前远端：旧 37 项全部完全一致，仅新增 Sol/Astra 两项；另检查三项 token 最大值和努力级别。工件 SHA256：`6ef05f244e9a726afe34650cdfe01b93414b194434fd18e774a3ba35421a2e33`。

本次只准备本地发布工件，未推送 `model-profiles` 分支、未发布远端 JSON、未修改 ZCode 仓库或客户端二进制。

## 已完成验证

- `go test -tags=unit -p 2 ./internal/service -run 'TestDesktopAgent(GPT62|BuiltinProfiles|Vendor|Official|RemoteCatalog)' -count=1` 通过。
- 实际运行导出 CLI 成功；JSON 39 项、envelope revision、两模型官方最大值/努力级别以及旧 37 项无变化的断言通过。
- 前端定向 5 个测试文件、**34 个测试全部通过**：`groupsOpenAIFast`、`GroupsView.openaiForcedTier`、`ChannelsView.ultrafastPricing`、`PricingEntryCard.modelDefaultPrice`、`PricingEntryCard.timePricing`。覆盖渠道创建/回填/修改/清空/非法值/平台门控及分组开关实际 payload。
- 已完成上述 SQL 字节比对。全量后端、集成测试、前端其余检查及总合并验收由主审计统一记录。

前端定向运行出现既有 Browserslist 数据陈旧提示，以及浅挂载 stub 的 Vue attributes/listeners 提示；未出现失败断言。本记录不把本地测试等同于真实供应商在线验收。
