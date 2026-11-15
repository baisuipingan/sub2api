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

`key` 只允许以下路径：`milocode/releases/<稳定semver>/win32-x64/<exe或exe.blockmap>`，`milocode/releases/<稳定semver>/<darwin-arm64或darwin-x64>/<zip或dmg>`，`milocode/stable/win32-x64/latest.json`，`milocode/stable/<darwin-arm64或darwin-x64>/appcast.xml`。路径版本必须等于请求 version；不接受 traversal、URL、额外斜杠、编码路径或任意后缀。版本资源仅允许 `if_none_match:"*"`；stable 元数据允许首次创建或带 ETag 的条件覆盖。URL 签名固定对象、长度、类型、cache-control、版本/hash metadata 和条件头。此接口不提供通用桶写入能力。

发布私钥留在发布机；S3 Secret 留在后端。发布 CLI 用短时管理员会话申请 PUT URL，然后流式直传 R2，不能把管理员 token 发往 R2。开启 step-up 时使用已二次验证的 JWT 会话；不得为机器凭据绕过原门控。

时序：本地构建 → 本地签名并生成不可变计划 → 读取后端配置并核对 base → 预检每个对象及版本冲突 → 逐个申请条件 PUT 并直传 → 验证远端大小、hash metadata、公网 Range 能力及完整包的流式 SHA512 → 最后条件更新 stable feed。失败保留旧 feed，重复请求不覆盖同版本不同字节。公网完整包校验由发布机执行，不仅检查对象声明的 metadata 或首字节。Sub2API 不接收或转发安装包正文，不承担客户端下载流量。

连接测试只使用随机小对象，网络访问必须复用现有 URL/SSRF 限制且禁跟随重定向，成功或失败均尝试删除该测试对象，绝不操作正式安装包。不要对公开域名根目录的 404 当作配置失败；需用实际测试对象确认。

验证：设置默认关闭、存取/清空/校验；备份 Secret 不外泄且不复制；普通用户与受限管理员拒绝；step-up 路由；路径白名单与条件写不可绕过；前端懒加载、草稿测试、独立保存及失败后保留输入；发布 CLI 确认 base 不符时零写入、token 不到 R2、上传资源先于 feed。真实公网上传和两平台完整应用升级另行报告，不以 mock 代替。

## 网站下载入口

顶部通知铃铛前提供整块可点击的 MiloCode 推广入口，左侧菜单不再重复展示。控制台、工作台及管理员均可进入同一个下载页面，并保留当前工作区及角色上下文；仅后端模式仍沿用原来的用户访问限制。宽屏显示客户端提示与下载图标，窄屏保留紧凑品牌入口。页面沿用站内布局、按钮、加载与空状态组件，列出 Windows x64、macOS Apple Silicon（ARM64）和 macOS Intel（x64）。每个平台展示自己已发布的版本；不把版本号或安装包地址写死在前端。无配置、尚未发布或元数据读取失败时，显示明确的不可下载状态，不能生成猜测的安装包链接。

公开轻量接口 `GET /api/v1/desktop/downloads` 返回 `{platforms:[{platform,available,version?,url?,size?,format?}]}`。它从已配置公开域名读取固定的 `milocode/stable/win32-x64/latest.json`、`milocode/stable/darwin-arm64/appcast.xml` 和 `milocode/stable/darwin-x64/appcast.xml`，不访问 S3 凭据或枚举桶。读取限制 1 MiB、短超时，复用 SSRF 校验且不跟随重定向；请求合并并在进程内缓存 60 秒，公开入口沿用面板 IP 限流。只有同一个 HTTPS origin 下、与平台和稳定版本一致的固定 releases 路径可作为下载地址。Windows 读取签名 envelope 中的 EXE 信息；Mac 从各自架构的 Sparkle 完整 ZIP enclosure 发现可信的已发布版本并忽略 delta 包，再对同版本固定路径 `milocode/releases/<version>/darwin-<arch>/MiloCode-<version>-mac-<arch>.dmg`（arch 为 arm64 或 x64）作最长 2 秒的公开 HEAD 确认。只有 200、合法正数大小（不超过发布上限）、`application/x-apple-diskimage` 类型才对官网返回 DMG URL/大小/格式；不转发响应正文或跟随任何重定向。DMG 不存在、超时或元数据不合法时兼容旧版 ZIP，并在页面明确说明手动将应用移入「应用程序」，不输出未经确认的 DMG 猜测链接。

官网手动安装与应用内更新分别使用适合自己的包：新版本官网默认下载 DMG，打开后将 MiloCode 拖到 Applications；Sparkle appcast 的更新 enclosure 始终保持已签署 ZIP，不因为官网安装体验改变更新格式、签名或公钥。HEAD 结果与版本元数据一并缓存 60 秒，每次页面访问不会重复探测 DMG，更不会经服务器下载整个安装包。

网页下载与客户端更新的信任边界不同：网页和普通软件下载站一样信任管理员配置的 HTTPS 发布源，后端不持有客户端公钥、不声称替代客户端验签。已安装客户端继续用构建时固定的密钥验证签名 feed 和更新包。网站接口只输出公开版本、安装包 URL 和大小，不输出 bucket、S3 endpoint、凭据、PUT 签名 URL、私钥或原始 feed 内容。点击下载后由浏览器直接请求公开发布源，Sub2API 不代理包正文。

下载页使用常规 HTTP 缓存，最多 60 秒出现新版本；后端配置变更立即切换源，停用配置立即返回不可用。三个平台互相独立，单个平台未发布或 feed 异常不阻断其他平台的下载。验证覆盖最新版本解析、同源与路径校验、未知/过大/失败 feed、并发缓存、平台独立、顶部入口及空状态。

### Mac 安装辅助

下载页说明当前 Mac 包有 ad-hoc 完整性签名，但没有 Apple 开发者签名及公证，提供同站静态文件 `/downloads/MiloCode-macOS-Helper.command`。源文件在 `frontend/public/downloads/`，Vite 构建后随前端嵌入服务器，不需要额外存储桶或服务端执行脚本。

用户先打开 DMG，将 MiloCode 拖入窗口中的 Applications；旧版只有 ZIP 时，解压后将应用移入「应用程序」。随后下载脚本，在终端运行 `bash ~/Downloads/MiloCode-macOS-Helper.command`。脚本只接受 `/Applications/MiloCode.app` 或 `~/Applications/MiloCode.app`，核对 `app.milo.code` / `MiloCode` 标识、拒绝异常链接路径并验证现有代码签名；用户确认后仅递归删除该应用的 `com.apple.quarantine` 并打开应用。它不会重新签名、默认提权、下载远端程序、清空全部扩展属性或关闭 Gatekeeper。完整性验证不等同于 Apple 认证发布者，仍需确认安装包和脚本来自本站。权限或完整性检查失败时给出明确说明，保留系统设置中的「仍要打开」作为替代路径。

### 并发下载与 Intel Mac

下载页支持 Windows x64、Mac Apple Silicon（ARM64）和 Mac Intel（x64）三个独立目标。Intel 使用 `milocode/stable/darwin-x64/appcast.xml`，安装包为 `milocode/releases/<version>/darwin-x64/MiloCode-<version>-mac-x64.{zip,dmg}`。沿用同源、稳定版本、路径白名单与实际 DMG HEAD 检查，不从 ARM64 feed 推测 Intel 安装包，也不因某一平台未发布而隐藏其他平台。

支持文件保存 API 的浏览器可选择加速下载：最多 6 个请求并发读取 4 MiB 分段，以位置写入用户选定的文件，不在内存组装整个安装包。每段必须返回准确的 206、Content-Range、长度及一致的 ETag，失败有限重试；中断时终止未完成写入。用户取消不会启动其他下载；读取或写入失败时静默使用原有普通下载入口。普通直链始终可用，不支持文件保存 API 的浏览器直接使用普通下载。页面只展示下载进度与取消操作，不展示运维配置或降级提示。

安装包仍由浏览器直连 R2/CDN，Sub2API 和 Nginx 不转发包正文。分段请求不保证使用不同 Cloudflare IP 或固定提速倍数；一个安装包对应多个 GET，请求数高于普通下载。

下载桶的 Wrangler CORS 配置在 `deploy/cloudflare/milocode-releases-cors.json`。配置仅允许本站 GET/HEAD 及 Range 请求头，并暴露分段校验需要的响应头；不授权浏览器上传或修改对象。应用前先读取并保留现有策略，若存在其他合法规则则合并，不能覆盖无关配置：

```sh
npx wrangler r2 bucket cors list milocode-releases
npx wrangler r2 bucket cors set milocode-releases --file deploy/cloudflare/milocode-releases-cors.json
```

配置后核对预检及带 `Origin: https://milorouter.com` 的 Range GET。若自定义域名已有旧缓存，应清除下载主机名的缓存，或通过单文件清除 API 携带正确的 Origin 缓存键后再次验证；控制台仅按 URL 清除可能遗漏旧的跨域缓存变体。不能以带随机 query 的新 URL 成功代替原始正式链接验收。CORS 由 R2 下载域名返回；在主站 Nginx 增加 CORS 头不会改变 R2 响应。参考 [R2 CORS](https://developers.cloudflare.com/r2/buckets/cors/) 和 [Cloudflare 缓存与 CORS](https://developers.cloudflare.com/cache/cache-security/cors/)。

2026-11-15 已通过 Wrangler 为 `milocode-releases` 应用上述规则，并仅刷新 `download.editor.vip` 的缓存。正式 Windows 3.17.8 EXE 和 ARM64 DMG 的原始 URL 已验证：预检 204，Range GET 206，允许 Origin 为 `https://milorouter.com`，暴露 Content-Range、Content-Length、Accept-Ranges、ETag。Windows 非首段携带 If-Range 请求同样返回正确的 206 / Content-Range，缓存 HIT 后仍保留 CORS 头。此记录只确认桶配置和现有包的读取链路，不表示本轮网站代码或 Intel 安装包已发布。

独立 Chrome 验收使用 `https://milorouter.com` 页面来源，实际读取正式 Windows 包的 1 byte 探测及 6 个并发 4 MiB 分段；7 个请求均返回 206，浏览器可读取准确的 Content-Range、Content-Length 和一致 ETag，请求不含 Cookie 或 Authorization。共读取 24 MiB + 1 byte，此次网络样本耗时约 5.7 秒，不作为其他地区线路的提速承诺。下载页以真实组件及隔离夹具验收中英文、明暗主题和 320–1536 px 共 14 个布局场景，无页面/卡片/按钮溢出，无 CORS 或降级运维提示；Intel 卡片为未发布平台的测试夹具。临时入口、测试浏览器和 Vite 服务均已清理，证据保存在 `output/playwright/milocode-download-*-20261115*`。

## 2026-11-15 并发下载与 Intel Mac 生产发布

网站于 `2026-11-15T07:36:40Z` 部署为 `0.2.21-ds.21`，发布标签 `fork-v0.2.21-21`，源码 `f5e94db9b6c0138bb570570f1afb28c162600ee4`。[CI 43688659838](https://github.com/baisuipingan/sub2api/actions/runs/43688659838) 的后端测试、lint、前端测试、类型检查、构建及发布脚本检查全部通过后，使用产出的不可变镜像 `ghcr.io/baisuipingan/sub2api@sha256:986d3442c2ca02c56ffefbff51c2794ac5e8bfa48d0188b1fc96f69078091254`，经生产既有部署入口发布。

本次没有数据库迁移，schema epoch 仍为 4，保持 `backup-required` 策略。新备份 `/root/sub2api-deploy/releases/backups/sub2api-20261115T073631Z.dump.gz` 为 15,972,580 bytes，gzip 与 PGDMP 标识已验证；旧版本精确镜像保留在 `previous.json`。服务、PostgreSQL 和 Redis 均 healthy，本地、公网健康检查均为 ok。Agent、软件发布存储、图库、备份存储及备份计划五项设置的哈希与发布前一致，原 Windows／Apple Silicon 的 feed 正文与下载元数据也完全一致。部署与阶段验收证据在 `output/deploy/fork-v0.2.21-21/`。

第一次候选标签 `fork-v0.2.21-20` 的后端测试通过，但 lint 发现仅剩测试引用的旧 ARM64 解析包装函数，因此未构建或部署该候选。删除遗留函数、将测试直接指向实际使用的架构解析函数后，创建新标签完成完整 CI；未覆盖旧标签或跳过门禁。

MiloCode Intel Mac `3.17.8` 随后从客户端已提交源码 `020b36c7653d2150836fa7870c986fc9eb3616a9` 独立构建发布，未重发 Windows／Apple Silicon。官网 DMG 为 `https://download.editor.vip/milocode/releases/3.17.8/darwin-x64/MiloCode-3.17.8-mac-x64.dmg`，245,379,861 bytes，SHA256 `6d5adb9ea1ba0ac15db6426318fa392dc194fb20028e887e7c31e08aa70766d7`；更新 ZIP 为 237,728,242 bytes，SHA256 `68ec605b1f12bd2b842bc1df0acfe91f45ddb4279be32c3948780ff4394d1116`，使用独立 `stable/darwin-x64/appcast.xml` 和原 Sparkle 签名身份。发布工具在写入 feed 前完成两个包的公网完整 SHA512 校验，固定公钥独立验签通过。生产最终元数据已确认三平台均可下载 3.17.8，Intel 为经 HEAD 确认的 DMG；原两个 feed 和五项设置哈希仍未改变。

生产来源的独立 Chrome 实测 Windows 199,559,833-byte 安装包分段下载并完整保存／回读，49 次进度事件单调递增，测试文件已删除；另一测试在写入首个 4 MiB 后取消，收到 AbortError，并确认事务写入回滚保留原测试文件。文件 API 验收使用隔离浏览器的 Origin Private File System，未改动用户文件；生产已登录页面另行检查真实三平台入口。证据位于 `output/playwright/production-fork-v0.2.21-21/`。

Intel 包通过 28 项静态检查及全量原生架构扫描，27 个 Mach-O 均支持 x86_64；当前 Apple Silicon 发布机无可用 Intel 执行环境，未宣称 Intel 实机启动、登录、Computer Use 或应用内升级已通过。系统下限为 macOS 13，仍为免费 ad-hoc 完整性签名、未 Apple 公证，沿用现有安装辅助说明。

## 2026-11-14 官网 DMG 发布记录

官网 Mac 手动下载优先 DMG 的变更已于 `2026-11-14T09:54:49Z` 部署至生产，网站版本 `0.2.21-ds.19`，发布标签 `fork-v0.2.21-19`，源码 `852bcc50498fecfb41177d63bb63d5de405e644f`。使用 [CI 43580811372](https://github.com/baisuipingan/sub2api/actions/runs/43580811372) 全部成功后产出的不可变镜像 `ghcr.io/baisuipingan/sub2api@sha256:6dcfb4085727da5bcdf58ec6733f63a457a230e4cf6cb17e7fdde0aa22d3c1f1`，由生产既有 `sub2api-release deploy` 入口部署，没有服务器现场构建或跳过发布门禁。

此次没有数据库迁移，schema epoch 保持 4，仍沿用 `backup-required` 回退策略。部署前备份 `/root/sub2api-deploy/releases/backups/sub2api-20261114T095435Z.dump.gz` 为 15,321,597 bytes，gzip 与 PGDMP 格式已验证。主应用、PostgreSQL、Redis 全部 healthy，本地及公网 health 均为 ok。部署记录、镜像标签、备份与下载接口证据位于 `output/deploy/fork-v0.2.21-19/`。

本轮相关验证包括 Go 下载 / 发布接口定向测试、前端 10 项回归、类型检查、完整 CI 的后端和前端测试 / lint / 构建。生产 Go HTTP 客户端实际读取 DMG HEAD 为 200，类型和长度合法且约 0.2 秒；官网接口已从 Mac ZIP 切换为经确认存在的 DMG，不改变客户端 Sparkle ZIP 更新链路。

MiloCode 3.17.6 正式发布后再次检查，生产 `/api/v1/desktop/downloads` 两平台均为 `3.17.6`：Windows `exe` 为 198,091,170 bytes；Mac `dmg` 为 235,607,781 bytes，生产 HEAD 为 200 / `application/x-apple-diskimage`，长度完全匹配。已登录 Chrome 下载页也已核对两平台版本、链接和 Mac 拖拽安装说明，证据 `production-release-result-final.json`。客户端安装包、公网完整字节与更新签名的独立验证记录位于客户端仓库 `docs/fork/38-client-release-3.17.6.md`。
