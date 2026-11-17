# 官方 v0.2.22 整合版生产发布验收

发布日期：2026-11-17。生产已完成从 `0.2.21-ds.21` 到 `0.2.22-ds.1` 的切换。

## 发布身份

- 官方基线：`v0.2.22` / `b0fa9f3e6eeab1968a53e1289b69daa4bb40ca2b`。
- 发布标签：`fork-v0.2.22-1`。
- 发布源码：`b0597931bdf905ec50ba33653050da22f4db5474`，分支 `codex/sync-v0.2.22`。
- 生产镜像：`ghcr.io/baisuipingan/sub2api@sha256:be12542a178a1b05388ade01301bfb692541cfdb232d22a08c252fe862d50dfd`。
- 正式发布 CI：[Fork Release 44100277879](https://github.com/baisuipingan/sub2api/actions/runs/44100277879)，验证与镜像任务全部成功。
- 生产记录切换时间：`2026-11-17T14:36:34Z`。

本次将官方优化接入定制积分链路的范围、SQL 写次数和并发验证，见 [整合审计](SYNC_V0_2_22_INTEGRATION_AUDIT.md)、[转发审计](SYNC_V0_2_22_FORWARDING_AUDIT.md) 和 [定价与界面审计](SYNC_V0_2_22_PRICING_UI_AUDIT.md)。当前生产使用 `credits`，聚合结算无需额外启用配置。

## 发布前检查

- 两轮独立审查未发现阻断问题，分别覆盖积分结算与其余官方整合。
- 正式发布执行完整后端 unit、integration 和 lint；前端 482 个测试文件、4,150 个测试全部通过，另有 3 个独立检查测试通过；typecheck、生产构建和发布脚本验证通过。
- 315 份已应用迁移的文件名和 checksum 与源码一致。仅两份官方迁移待执行，未修改历史迁移。
- 切换前再次确认：无排队、生成、保存中的 Studio 任务及活动子调用；无数据库锁等待或超过 30 秒的事务；钱包 USD/积分投影不一致数为 0。
- 记录 Agent、客户端发布存储、图库/异步图片存储、备份及积分配置的哈希，不在验收文件中保存这些配置的明文。

## 生产变更与备份

使用已安装的 `sub2api-release` 部署脚本，校验正式发布 manifest 的标签、源码、官方基线、schema policy 和不可变摘要后执行。只重建主应用服务，数据库和 Redis 镜像保持原样。

- 切换前数据库备份：`/root/sub2api-deploy/releases/backups/sub2api-20261117T143625Z.dump.gz`。
- 压缩备份大小：16,717,892 字节，权限 `0600`。
- 脚本检查 gzip 和 PostgreSQL custom dump 格式；发布后另以 `pg_restore --list` 成功读取 1,335 个目录条目。该检查不等同于另建数据库进行完整恢复演练。
- 上一版精确镜像保存在 `previous.json`，摘要为 `sha256:986d3442c2ca02c56ffefbff51c2794ac5e8bfa48d0188b1fc96f69078091254`。
- 沿用已有发布保留策略：最近 5 份数据库备份、10 份版本记录及 3 个本地发布镜像。
- Schema epoch 保持 4，回退策略仍为 `backup-required`，不可直接绕过策略只降级镜像。

本次新增应用：

1. `251_group_force_openai_ultrafast.sql`：旧分组默认 `false`，没有主动开启 UltraFast，也未改变既有分组档位或价格。
2. `251_group_usage_rollup_nonblocking_build.sql`：统计状态新增 3 列，历史范围与已有保留策略保持原有语义，后台自动接管重建，无需用户配置。

## 上线验收

- 运行镜像、容器标签、二进制版本、源码 SHA、manifest 和当前发布记录完全一致。
- 本机及服务器访问公开健康接口、服务器访问本地健康接口均为 `ok`；应用、PostgreSQL、Redis 健康。
- 两份迁移成功执行，原有 315 份迁移 checksum 保持一致。
- 所核对配置哈希完全一致；Windows、Apple Silicon 的更新 feed 与下载元数据保持一致，Intel Mac 的独立 feed 和 DMG 元数据也通过检查。
- 生产页面显示 `v0.2.22-ds.1`。使用记录模型分布可展开用户用量，包含请求、Token、实际消费、成本、标准列，并实际返回用户数据。
- OpenAI 分组编辑页能显示 UltraFast 开关，确认关闭后取消，未保存任何分组配置；数据库启用 UltraFast 的分组数为 0。
- 管理工作台概览、生成任务及实际路由列正常；图库列表 12 张缩略图全部成功加载。
- 用户图片生成页加载 5 个模型，`gpt-image-2` 参数正常，初始 2K 被选中，历史任务正常加载。未额外提交付费生成请求。
- 客户端下载页正常加载 Windows x64、Mac ARM64、Mac Intel x64，均继续指向既有 3.17.8 安装包。
- 浏览器验收过程中未捕获到 console error。
- 上线后观察到 17 笔自然生产调用、17 笔钱包流水，逐笔与 usage log 关联，积分扣款不一致数为 0；全站钱包 USD/积分投影不一致数为 0。
- 观察窗口内未检出 panic/fatal、PostgreSQL、迁移或结算错误。

验收为上述时间窗口和页面范围的结果，不将其解释为所有供应商、所有模型的付费端到端调用都已重新验证。

原始安全摘要和可复跑脚本保存在忽略目录 `output/deploy/fork-v0.2.22-1/`；本记录不包含密钥、用户提示词、私有图片链接或配置明文。
