# MiloCode 发布存储

桌面端设置增加独立的软件发布存储卡片。固定复用备份 S3 的 endpoint、region、凭据和 path-style，只由管理员填写独立 bucket 与公开域名，不复制凭据。默认空配置，未配置不影响原网关、Agent、备份、图库或既有下载。

配置使用 settings 中的 `desktop_release_storage_v1` JSON，不增加表，不混入 Agent bootstrap 或模型 revision。桶必须显式填写且不能等于备份桶；公开域名为不含凭据、query、fragment、路径的 HTTPS origin。固定前缀 `milocode/`。当前用户提供的目标是 `milocode-releases`、`https://download.editor.vip`，表单不擅自保存。

管理 API 统一在 `/api/v1/admin/desktop/release-storage` 下，沿用完整管理员鉴权；PUT 配置和签发上传 URL 沿用现有 step-up。所有响应 no-store，错误不包含 S3 密钥、签名 URL 或底层响应正文。

| 方法与后缀 | 请求 | data 响应 |
| --- | --- | --- |
| GET 空后缀 | 无 | `{bucket, public_base_url, release_base_url, backup_s3_configured, configured}` |
| PUT 空后缀 | `{bucket,public_base_url}` | 同 GET；两项同时清空可停用发布配置 |
| POST `/test` | `{bucket,public_base_url}` | `{success:true}`；使用草稿测试，先验证继承凭据再执行可清理的小对象读写与公网映射检查 |
| POST `/objects/head` | `{key}` | `{exists:false}` 或 `{exists:true,size,sha512,version,etag}` |
| POST `/uploads` | `{key,size,content_type,sha256,sha512,version,if_none_match?,if_match?}` | `{url,headers,expires_at}`，URL 有效期 15 分钟；headers 是直传时必须保留的签名约束 |

`release_base_url` 为 `<public_base_url>/milocode/`，发布工具在任何写入前必须核对它与本地已签署清单的 baseUrl 一致。客户端正式源和公钥在构建时固化，修改后台域名不应远程接管已安装客户端。

`key` 只允许以下路径：`milocode/releases/<稳定semver>/win32-x64/<exe或exe.blockmap>`，`milocode/releases/<稳定semver>/darwin-arm64/<zip或dmg>`，`milocode/stable/win32-x64/latest.json`，`milocode/stable/darwin-arm64/appcast.xml`。路径版本必须等于请求 version；不接受 traversal、URL、额外斜杠、编码路径或任意后缀。版本资源仅允许 `if_none_match:"*"`；stable 元数据允许首次创建或带 ETag 的条件覆盖。URL 签名固定对象、长度、类型、cache-control、版本/hash metadata 和条件头。此接口不提供通用桶写入能力。

发布私钥留在发布机；S3 Secret 留在后端。发布 CLI 用短时管理员会话申请 PUT URL，然后流式直传 R2，不能把管理员 token 发往 R2。开启 step-up 时使用已二次验证的 JWT 会话；不得为机器凭据绕过原门控。

时序：本地构建 → 本地签名并生成不可变计划 → 读取后端配置并核对 base → 预检每个对象及版本冲突 → 逐个申请条件 PUT 并直传 → 验证远端大小、hash metadata、公网 Range 能力及完整包的流式 SHA512 → 最后条件更新 stable feed。失败保留旧 feed，重复请求不覆盖同版本不同字节。公网完整包校验由发布机执行，不仅检查对象声明的 metadata 或首字节。Sub2API 不接收或转发安装包正文，不承担客户端下载流量。

连接测试只使用随机小对象，网络访问必须复用现有 URL/SSRF 限制且禁跟随重定向，成功或失败均尝试删除该测试对象，绝不操作正式安装包。不要对公开域名根目录的 404 当作配置失败；需用实际测试对象确认。

验证：设置默认关闭、存取/清空/校验；备份 Secret 不外泄且不复制；普通用户与受限管理员拒绝；step-up 路由；路径白名单与条件写不可绕过；前端懒加载、草稿测试、独立保存及失败后保留输入；发布 CLI 确认 base 不符时零写入、token 不到 R2、上传资源先于 feed。真实公网上传和两平台完整应用升级另行报告，不以 mock 代替。

## 网站下载入口

顶部通知铃铛前提供整块可点击的 MiloCode 推广入口，左侧菜单不再重复展示。控制台、工作台及管理员均可进入同一个下载页面，并保留当前工作区及角色上下文；仅后端模式仍沿用原来的用户访问限制。宽屏显示客户端提示与下载图标，窄屏保留紧凑品牌入口。页面沿用站内布局、按钮、加载与空状态组件，仅列出 Windows x64 和 macOS Apple Silicon（ARM64）。每个平台展示自己已发布的版本；不把版本号或安装包地址写死在前端。无配置、尚未发布或元数据读取失败时，显示明确的不可下载状态，不能生成猜测的安装包链接。

公开轻量接口 `GET /api/v1/desktop/downloads` 返回 `{platforms:[{platform,available,version?,url?,size?,format?}]}`。它从已配置公开域名读取固定的 `milocode/stable/win32-x64/latest.json` 与 `milocode/stable/darwin-arm64/appcast.xml`，不访问 S3 凭据或枚举桶。读取限制 1 MiB、短超时，复用 SSRF 校验且不跟随重定向；请求合并并在进程内缓存 60 秒，公开入口沿用面板 IP 限流。只有同一个 HTTPS origin 下、与平台和稳定版本一致的固定 releases 路径可作为下载地址。Windows 读取签名 envelope 中的 EXE 信息；Mac 从 Sparkle 完整 ZIP enclosure 发现可信的已发布版本并忽略 delta 包，再对同版本固定路径 `milocode/releases/<version>/darwin-arm64/MiloCode-<version>-mac-arm64.dmg` 作最长 2 秒的公开 HEAD 确认。只有 200、合法正数大小（不超过发布上限）、`application/x-apple-diskimage` 类型才对官网返回 DMG URL/大小/格式；不转发响应正文或跟随任何重定向。DMG 不存在、超时或元数据不合法时兼容旧版 ZIP，并在页面明确说明手动将应用移入「应用程序」，不输出未经确认的 DMG 猜测链接。

官网手动安装与应用内更新分别使用适合自己的包：新版本官网默认下载 DMG，打开后将 MiloCode 拖到 Applications；Sparkle appcast 的更新 enclosure 始终保持已签署 ZIP，不因为官网安装体验改变更新格式、签名或公钥。HEAD 结果与版本元数据一并缓存 60 秒，每次页面访问不会重复探测 DMG，更不会经服务器下载整个安装包。

网页下载与客户端更新的信任边界不同：网页和普通软件下载站一样信任管理员配置的 HTTPS 发布源，后端不持有客户端公钥、不声称替代客户端验签。已安装客户端继续用构建时固定的密钥验证签名 feed 和更新包。网站接口只输出公开版本、安装包 URL 和大小，不输出 bucket、S3 endpoint、凭据、PUT 签名 URL、私钥或原始 feed 内容。点击下载后由浏览器直接请求公开发布源，Sub2API 不代理包正文。

下载页使用常规 HTTP 缓存，最多 60 秒出现新版本；后端配置变更立即切换源，停用配置立即返回不可用。两平台互相独立，单个平台未发布或 feed 异常不阻断另一个平台的下载。验证覆盖最新版本解析、同源与路径校验、未知/过大/失败 feed、并发缓存、平台独立、顶部入口及空状态。

### Mac 安装辅助

下载页说明当前 Mac 包有 ad-hoc 完整性签名，但没有 Apple 开发者签名及公证，提供同站静态文件 `/downloads/MiloCode-macOS-Helper.command`。源文件在 `frontend/public/downloads/`，Vite 构建后随前端嵌入服务器，不需要额外存储桶或服务端执行脚本。

用户先打开 DMG，将 MiloCode 拖入窗口中的 Applications；旧版只有 ZIP 时，解压后将应用移入「应用程序」。随后下载脚本，在终端运行 `bash ~/Downloads/MiloCode-macOS-Helper.command`。脚本只接受 `/Applications/MiloCode.app` 或 `~/Applications/MiloCode.app`，核对 `app.milo.code` / `MiloCode` 标识、拒绝异常链接路径并验证现有代码签名；用户确认后仅递归删除该应用的 `com.apple.quarantine` 并打开应用。它不会重新签名、默认提权、下载远端程序、清空全部扩展属性或关闭 Gatekeeper。完整性验证不等同于 Apple 认证发布者，仍需确认安装包和脚本来自本站。权限或完整性检查失败时给出明确说明，保留系统设置中的「仍要打开」作为替代路径。

## 2026-11-14 官网 DMG 发布记录

官网 Mac 手动下载优先 DMG 的变更已于 `2026-11-14T09:54:49Z` 部署至生产，网站版本 `0.2.21-ds.19`，发布标签 `fork-v0.2.21-19`，源码 `852bcc50498fecfb41177d63bb63d5de405e644f`。使用 [CI 43580811372](https://github.com/baisuipingan/sub2api/actions/runs/43580811372) 全部成功后产出的不可变镜像 `ghcr.io/baisuipingan/sub2api@sha256:6dcfb4085727da5bcdf58ec6733f63a457a230e4cf6cb17e7fdde0aa22d3c1f1`，由生产既有 `sub2api-release deploy` 入口部署，没有服务器现场构建或跳过发布门禁。

此次没有数据库迁移，schema epoch 保持 4，仍沿用 `backup-required` 回退策略。部署前备份 `/root/sub2api-deploy/releases/backups/sub2api-20261114T095435Z.dump.gz` 为 15,321,597 bytes，gzip 与 PGDMP 格式已验证。主应用、PostgreSQL、Redis 全部 healthy，本地及公网 health 均为 ok。部署记录、镜像标签、备份与下载接口证据位于 `output/deploy/fork-v0.2.21-19/`。

本轮相关验证包括 Go 下载 / 发布接口定向测试、前端 10 项回归、类型检查、完整 CI 的后端和前端测试 / lint / 构建。生产 Go HTTP 客户端实际读取 DMG HEAD 为 200，类型和长度合法且约 0.2 秒；官网接口已从 Mac ZIP 切换为经确认存在的 DMG，不改变客户端 Sparkle ZIP 更新链路。

MiloCode 3.17.6 正式发布后再次检查，生产 `/api/v1/desktop/downloads` 两平台均为 `3.17.6`：Windows `exe` 为 198,091,170 bytes；Mac `dmg` 为 235,607,781 bytes，生产 HEAD 为 200 / `application/x-apple-diskimage`，长度完全匹配。已登录 Chrome 下载页也已核对两平台版本、链接和 Mac 拖拽安装说明，证据 `production-release-result-final.json`。客户端安装包、公网完整字节与更新签名的独立验证记录位于客户端仓库 `docs/fork/38-client-release-3.17.6.md`。
