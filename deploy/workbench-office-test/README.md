# 独立工作台验收环境

仅用于测试机 `/opt/milo-workbench-test`。Compose 项目名固定 `milo-office-test`，不加入已有服务的网络，不复用已有数据库、Redis、RustFS、S3 或生产数据。所有凭证在远端独立生成，保存到 `secrets/compose.env`（0600），禁止提交到仓库。镜像解析为摘要后记录到该文件；不得把 `docker compose config` 的完整输出写入日志。

部署目录包含 `stack/compose.yml`、`secrets/compose.env`、`source/` 和 `logs/`。浏览器推进对话，Sub2API 调模型并读取对话历史。文档编辑交给内网 `office-tools` 服务，成品写入测试对象存储。

初始化只启动独立存储：

```sh
docker compose --env-file ../secrets/compose.env up -d postgres redis minio minio-init
docker compose --env-file ../secrets/compose.env ps
```

构建主项目的 app 镜像，并通过 `build_images.sh` 构建 `services/workbench-office-tools`。完成检查后，从 `stack` 目录启动 `docker compose --env-file ../secrets/compose.env --profile app up -d`。`office-tools` 随 app 启动，应用端会用服务令牌检查文档服务连接。

仅 app 的 4000 和对象存储的 19000 绑定宿主 `127.0.0.1`，通过 SSH tunnel 访问。MinIO 管理控制台不发布，`office-test` 桶强制私有；Go 用内部 `http://minio:9000` 存储端点。浏览器验收需要运维在测试域名下提供同源对象反代，签名使用的 Host/路径必须与转发一致。不能把内网测试 URL 当作正式公网下载域名。

验收前配置测试分组、用户工作台 Key 和模型账户。测试使用受控模拟模型或明确授权的测试路由，文档服务默认同时处理一个任务。真实文档和原生协议的验收见 `test_native_protocol_api.py`。

旧部署升级时，按项目 `milo-office-test` 停止 `chat-worker`、`office-worker` 和 `executor` 容器，保留测试数据库、对象和历史任务记录。
