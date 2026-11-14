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

公开轻量接口 `GET /api/v1/desktop/downloads` 返回 `{platforms:[{platform,available,version?,url?,size?,format?}]}`。它只从已配置公开域名读取固定的 `milocode/stable/win32-x64/latest.json` 与 `milocode/stable/darwin-arm64/appcast.xml`，不访问 S3 凭据或枚举桶。读取限制 1 MiB、短超时，复用 SSRF 校验且不跟随重定向；请求合并并在进程内缓存 60 秒，公开入口沿用面板 IP 限流。只有同一个 HTTPS origin 下、与平台和稳定版本一致的固定 releases 路径可作为下载地址。Windows 读取签名 envelope 中的 EXE 信息；Mac 读取 Sparkle appcast 的完整 ZIP enclosure，忽略 delta 包，不假设可选 DMG 必然存在。

网页下载与客户端更新的信任边界不同：网页和普通软件下载站一样信任管理员配置的 HTTPS 发布源，后端不持有客户端公钥、不声称替代客户端验签。已安装客户端继续用构建时固定的密钥验证签名 feed 和更新包。网站接口只输出公开版本、安装包 URL 和大小，不输出 bucket、S3 endpoint、凭据、PUT 签名 URL、私钥或原始 feed 内容。点击下载后由浏览器直接请求公开发布源，Sub2API 不代理包正文。

下载页使用常规 HTTP 缓存，最多 60 秒出现新版本；后端配置变更立即切换源，停用配置立即返回不可用。两平台互相独立，单个平台未发布或 feed 异常不阻断另一个平台的下载。验证覆盖最新版本解析、同源与路径校验、未知/过大/失败 feed、并发缓存、平台独立、顶部入口及空状态。

### Mac 安装辅助

下载页说明当前 Mac 包有 ad-hoc 完整性签名，但没有 Apple 开发者签名及公证，提供同站静态文件 `/downloads/MiloCode-macOS-Helper.command`。源文件在 `frontend/public/downloads/`，Vite 构建后随前端嵌入服务器，不需要额外存储桶或服务端执行脚本。

用户先将 ZIP 中的应用移入「应用程序」，下载脚本后在终端运行 `bash ~/Downloads/MiloCode-macOS-Helper.command`。脚本只接受 `/Applications/MiloCode.app` 或 `~/Applications/MiloCode.app`，核对 `app.milo.code` / `MiloCode` 标识、拒绝异常链接路径并验证现有代码签名；用户确认后仅递归删除该应用的 `com.apple.quarantine` 并打开应用。它不会重新签名、默认提权、下载远端程序、清空全部扩展属性或关闭 Gatekeeper。完整性验证不等同于 Apple 认证发布者，仍需确认安装包和脚本来自本站。权限或完整性检查失败时给出明确说明，保留系统设置中的「仍要打开」作为替代路径。
