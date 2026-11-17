# 官方 v0.2.22 与定制功能的整合审计

审计日期：2026-11-17。官方范围：`v0.2.21..v0.2.22`，目标提交 `b0fa9f3e6`；定制合并前提交 `6acd21a8d`，第一版合并提交 `8d15af77c`。本记录同时覆盖第一版合并和后续补全。

## 修正第一版合并的不足

第一版将官方按用户排队、组批、去重与事务结算接到了积分计费，但积分批次在事务内仍逐条更新钱包、订阅和 Key。它保持了账目行为，却没有取得官方减少热点行 UPDATE 次数的核心收益。本次删除该兼容分支，将 `legacy`、`shadow`、`credits` 都接到精确聚合实现，积分模式无需切回 legacy。

| 数据 | 第一版积分批次 | 本次实现 |
| --- | --- | --- |
| 去重 | 批量认领 | 保留官方批量认领、指纹冲突和归档去重 |
| 钱包 | 每请求一次扣款 | 同一用户一次 UPDATE，按顺序还原每请求余额及透支标记 |
| 钱包版本 | 每请求递增 | 一次 UPDATE 增加实际积分扣款笔数 |
| 订阅 | 每请求一次更新 | 每订阅一次 UPDATE，同时保留旧 USD 计数第 9、10 位残差与切换引擎的顺序语义 |
| Key | 每请求分别更新额度和窗口 | 每 Key 一次 UPDATE，同时处理额度、状态、5h/1d/7d 窗口 |
| 账号额度 | 每请求一次更新 | 使用官方每账号一次更新与合并后的缓存失效通知 |
| 钱包流水 | 每请求一次 INSERT | 一次 INSERT 写入全部独立流水，保留各自余额、汇率、计费版本及 USD 快照 |

聚合算术用 `decimal`/PostgreSQL `numeric`，不使用 `int64` 累加总额；最终存储仍由 bigint/decimal 的边界约束保护。任意数据库错误会回滚本批钱包、额度、流水和去重认领。官方 lane 的整批失败后逐笔尝试仍保留，单笔账务接口与冻结资金接口没有被替换。

没有修改计费金额、积分比例、余额不足后的已放行请求透支政策，也没有新增数据库迁移。

## 全部上游改动的覆盖方法

上游本次涉及 340 个文件、108 个非 merge 提交；其中 46 个文件与定制改动重叠。核查同时使用三种证据：上游文件差异、定制入口的实际调用链、覆盖边界的回归测试。文件相同只证明实现被保留，调用链与组合测试用于确认定制功能确实使用它。

| 上游更新范围 | 定制链路整合结果 | 证据 |
| --- | --- | --- |
| 同用户聚合扣费、金额量化 | 积分钱包/订阅/Key 真正聚合，三种引擎可在同用户批次混用 | 本文数据库验证；`usage_billing_credit_batch_*_integration_test.go` |
| UltraFast 模型门控与倍率、Vertex 区域加价、DeepSeek 显式价格、Gemini Carbon/Hydrogen | 官方计价结果先计算，再生成同笔积分扣款与日志快照，订阅高峰/图片独立倍率继续生效 | `billing_credit_upstream_pricing_test.go`；[定价与界面审计](SYNC_V0_2_22_PRICING_UI_AUDIT.md) |
| 调度快照精简、共享解码、按 bucket 合并回源、白名单与 capabilities 字段 | 公共网关、云端工作台、MiloCode 共用快照选号，定制模型档位和会话信息保留 | [转发与调度审计](SYNC_V0_2_22_FORWARDING_AUDIT.md) |
| 请求体复用、Anthropic JSON 热路径优化 | 定制入口使用公共 router/handler；不存在另一套旧模型代理 | 同上；不将 multipart 图片处理宣称为全链路零拷贝 |
| keepalive、首输出超时/暂存降级、SSE 错误、兼容入口 failover 和账号副作用 | 公共流式文本链路直接继承，已输出内容后不会拼接另一账号答案；非流式图片仍按任务完成返回 | 同上及官方流式回归 |
| 组合分组的公开模型名还原、模型观察记录、端点诊断 | 公共路由逻辑保留，fork 使用记录的上游模型诊断字段继续存在 | 同上；管理/用户使用记录测试 |
| Gemini 粘性前缀、换号实际缓存用量、工具流、结构化输出；Codex 父缓存键、工具声明、untrusted context、工具 schema；Anthropic effort/beta 与加密工具内容 | 相关官方 service/handler 实现保留，MiloCode 与工作台进入公共转发；无额外定制协议分叉 | 官方 unit suite；两份专项审计 |
| group usage rollup 非阻塞重建、保留期清理 | 现有 usage_logs 写入与定制统计共用新实现；展示和用户下钻过滤继续保留 | 定价与界面审计；repository 集成套件 |
| 账号筛选下实际扣费与账号成本区分、账号用量刷新、分组 UltraFast/账号配置界面 | 定制分布图与官方 actual_cost 口径一致，原有每模型用户下钻保留 | DistributionBars 新组合回归及前端 suite |
| 连接失活检测、TCP_USER_TIMEOUT 容错、后台定价启动、禁用等待 ping | 官方 HTTP 客户端、设置和生命周期直接接入，定制远程能力档案同样后台刷新 | 转发审计；unit suite |
| Bedrock/Grok 错误与搜索用量、token 轮换、周额度恢复、RPM 作用域与事务、退款幂等 | 官方实现保留；没有用定制积分或 Studio 逻辑覆盖这些错误、调度、支付路径 | 上游差异核对及完整后端 suite |
| 用户成本字段隔离、HTML 缓存/CSP、登录容错、安全工具链 | 官方安全边界保留；fork DTO 只附加已存在的积分/用途字段 | 定价与界面审计；完整后端/前端/安全 CI |
| Ent/Wire、数据库迁移、容器与发布流程 | 新 UltraFast 字段及官方迁移完整保留，继续 fork 的版本元数据、WebP 工具及受管 Key 保护 | 生成代码/cleanup 测试；迁移 checksum 核查；CI |

各项能力按官方适用条件工作。例如仅默认价格应用 Vertex 加价，自定义最终价格不再叠加；仅适用模型使用 UltraFast；图片工作台的非流式请求不获得文本 SSE keepalive 效果。不能将“代码接入”理解成每个请求无条件启用所有功能。

## 真实数据库验证

测试使用临时 PostgreSQL 18.1 与 Redis 8.4，执行项目全部迁移。只对测试 fixture 安装临时数据库触发器计数，结束后移除，不修改生产数据库。

- 128 笔积分请求：钱包、同一订阅、同一 Key、同一账号各 1 次实际行 UPDATE；流水 1 条 INSERT 写 128 行。重放整批，计数不增加。
- 对比独立事务逐笔、单事务逐笔效果和聚合 SQL：数据库余额、版本、全部额度、每笔余额/状态及流水一致。耗时记录在测试日志，不用不稳定的时间倍数作断言。
- 同用户 300 并发积分请求：全部结算成功，每笔扣费后余额唯一，余额/版本/订阅/Key/300 条流水一致，batch fallback 为 0；原官方 USD 并发回归同样通过。
- legacy/shadow/credits 混批、显式积分与 USD 快照不同、NULL/过期/有效窗口、Key 删除、指纹冲突、归档去重、重复重放、缺失订阅/用户、负扣款、整数/decimal/版本溢出都进行验证。
- 两组历史订阅残差 × 七种引擎顺序与逐笔结算对照，覆盖负半积分跨零时 PostgreSQL 的舍入行为。
- UltraFast×订阅高峰、Vertex token 区域加价与不加价的存储费、DeepSeek 显式零价、图片独立倍率通过真实 RecordUsage 入口验证，最终 credits 命令和 usage log 快照一致。

本地一次最终定向测试的 128 笔结果：独立事务约 478ms，同事务逐笔效果约 673ms，聚合 SQL 约 16ms；300 并发约 36ms。它们仅说明该测试环境下的结算开销，不能当作生产端到端请求延迟或稳定性能倍数。

## 完整验证与交付

最终补全工作树已通过：完整后端 `go test -tags=unit -p 2 ./...`、完整后端 `go test -tags=integration -p 2 ./...`、前端 typecheck、336 个相关前端测试（含新增组合场景）、全部专项调度/转发/定价测试和 `git diff --check`。另由分支 CI 执行前端 lint/typecheck/关键用例、安全检查、Go lint 与发布脚本验证；其结果与提交关联，可在 GitHub Actions 查看。

专项日志保存在 ignored 的 `output/sync-v0.2.22-*.log`；可重跑的测试、SQL 写次数断言和调用链审计均随代码提交。

CI 补充：GitHub 将首次最终检查的 macOS ARM 任务排队后取消，注释明确为托管 runner 容量不足，未执行任何脚本。该组只做可移植的 shell/mock 检查，不编译架构相关程序，故将 runner 明确为同一 macOS 15 的 Intel 标签，保留全部检查。本机 ARM macOS 也已逐条执行同样的六项检查并通过。

本轮不发布 release、不迁移生产数据库。发布时仍需遵守 `.fork-release.json` 的 schema epoch 4 与备份回退要求；本次补全本身未增加新的表或配置项。

## 可复查的范围与命令

```sh
# 上游更新完整在合并历史中，不是挑选部分提交。
git merge-base --is-ancestor v0.2.22 HEAD
git diff --name-only v0.2.21 v0.2.22
git log --no-merges --format='%h %s' v0.2.21..v0.2.22

# 在 backend/ 执行；CI=true 使没有 Docker 时明确失败，避免跳过数据库测试。
go test -tags=unit -p 2 ./...
CI=true go test -tags=integration -p 2 ./...
go test -tags=integration -p 2 ./internal/repository \
  -run 'TestUsageBilling(RepositoryApplyBatch|CreditBatch|CreditLane|LaneCombiner)' -count=1 -v
go test -tags=unit ./internal/service \
  -run TestRecordUsageOfficialPricingFactorsReachCreditSnapshots -count=1 -v
```

340 个官方改动文件中，296 个在最终工作树直接保持官方内容，44 个存在需要保留的定制差异：

- 构建/发布：CI/security workflow、两个 Dockerfile、VERSION、go.mod；保留 fork 发布元数据、图库 WebP 依赖和更高版本的 image 解码依赖，并明确可用的 macOS 脚本执行机。
- 注入与生成：`cmd/server/wire*`、`service/wire.go`、Ent Group 及其生成文件；保留积分字段、定制服务及 cleanup，同时注入官方 lane 与 UltraFast。
- 领域与认证：constants、API Key repo/auth cache、admin_group；保留受管 Key 用途保护、积分余额和组额度，不覆盖新版组配置。
- 结算：usage_billing repo/batch、UsageBillingCommand、gateway_usage_billing；在此真正实现积分聚合，并保持官方价格因子传递。
- 统计/DTO/契约：dto、usagestats、trend、相关测试；保留积分展示、上游模型诊断及用户展开筛选，沿用官方 actual_cost 与成本隔离。
- 转发：OpenAI 两个出站构造器与 Grok media；差异见专项审计，官方转发主体仍共用。
- 前端：账号编辑、端点图、Groups、Usage、types 和对应中英文文案；保留币种与用户下钻，接入官方新字段与口径。

这里的“文件相同”不作为性能证明；真正的 SQL 写次数与定制入口组合回归在前文单独验证。
