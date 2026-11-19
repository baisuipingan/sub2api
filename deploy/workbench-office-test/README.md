# 独立工作台验收环境

仅用于测试机 `/opt/milo-workbench-test`。Compose 项目名固定 `milo-office-test`，不加入已有服务的网络，不复用已有数据库、Redis、RustFS、S3 或生产数据。所有凭证在远端独立生成，保存到 `secrets/compose.env`（0600），禁止提交到仓库。镜像解析为摘要后记录到该文件；不得把 `docker compose config` 的完整输出写入日志。

部署目录结构：`stack/compose.yml`、`secrets/compose.env`、`secrets/executor-token`、`source/`、`executor-state/`、`logs/`。`executor-state` 在宿主和 supervisor 必须是同一个绝对路径，因为任务容器由宿主 Docker 解释 bind 源路径。仅可信 supervisor 挂 Docker socket；Go、worker、用户任务均不挂。任务强制 gVisor、无网络、非 root、只读系统、受限 tmpfs，缺少运行时或固定镜像直接拒绝执行。

初始化只启动独立存储：

```sh
docker compose --env-file ../secrets/compose.env up -d postgres redis minio minio-init
docker compose --env-file ../secrets/compose.env ps
```

构建并完成静态检查后启动 `--profile app`；验证任务镜像的固定摘要及真实 Linux/gVisor 运行后才启动 `--profile workers`。app 镜像由主项目构建，worker 构建上下文是 `services/office-agent`，task/supervisor 构建上下文是仓库根目录，基镜像参数均使用摘要。

仅 app 的 4000 和对象存储的 19000 绑定宿主 `127.0.0.1`，通过 SSH tunnel 访问。MinIO 管理控制台不发布，`office-test` 桶强制私有；Go 用内部 `http://minio:9000` 存储端点。浏览器验收需要运维在测试域名下提供同源对象反代，签名使用的 Host/路径必须与转发一致。不能把内网测试 URL 当作正式公网下载域名。

验收前配置测试分组、用户工作台 Key 和模型账户，只允许受控模拟模型或明确授权的测试路由。不得导入生产数据库或复制生产模型/存储凭证。聊天与办公 worker 各一个，办公 executor 同时最多一个任务，避免与测试机其他服务抢占内存。

真实 Office 验收：使用专用 `executor-acceptance-state` 目录运行 `services/office-executor/tests/linux_acceptance.py --allow-task-execution`，不要与常驻 supervisor 共用 journal 锁。探针覆盖隔离、中文 DOCX、公式实际重算、PPTX、真实 Bisheng inspect/render、符号链接、超时和工作盘上限；生成的预览需另做视觉检查。单元测试通过不代表该项已经通过。

清理时仅操作此 Compose 项目、带 `milo.office-executor.namespace` 专用标签的任务和此目录；不得运行全局 `docker system prune`。保留测试日志/证据和当前需要的镜像，删除时使用该项目的显式名称。
