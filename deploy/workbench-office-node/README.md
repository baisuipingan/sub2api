# 部署办公算力节点

每台算力机运行一份 `office-tools` 容器。Sub2API 通过管理端登记节点并派发 Word、Excel、PPT、ZIP 和打印预览任务。每个节点用独立的令牌和持久卷。令牌保存在部署机器上的 `.env`，以及 Sub2API 的加密配置中。

## 启动第一台或新增节点

1. 将此目录和同一版本的 `services/workbench-office-tools` 带到算力机，保留仓库中的目录结构。在此目录创建 `.env`，以 `openssl rand -hex 32` 生成节点自己的 `OFFICE_TOOLS_TOKEN`，然后设置 `OFFICE_TOOLS_STORAGE_ORIGINS`。
2. 存在多个签名域名时用逗号分隔。如果 S3 使用路径寻址，R2 的常见值是 `https://<ACCOUNT_ID>.r2.cloudflarestorage.com`。若实际签名使用带桶名的主机名，填实际来源。从不同存储桶读取引用素材时，也加入其实际签名来源。
3. 执行 `docker compose up -d --build`。同版本多台机器也可以部署相同镜像。
4. 在 Sub2API 的「工作台管理 → 算力节点」登记地址和令牌，测试连接后启用。内网可用 HTTP。跨供应商机器可登记公网 HTTPS 域名，或登记使用受信证书且证书覆盖该地址的 HTTPS IP。
5. 在「工作台设置 → 办公文件存储」选用私有桶并测试。生产使用新桶，预览 CORS 放行工作台实际来源的 GET。用一份测试文档验证上传、编辑、预览和下载后开放使用。

节点使用短期签名直接向存储传输文件。正式文件和预览在对象存储，SQLite 回执、缓存和临时文件在节点的 `state` 卷中。Sub2API 使用现有持久化的 TOTP 加密密钥来保存节点令牌。

## 公网 HTTPS

Compose 默认监听宿主机的 `127.0.0.1:8094`。由 Nginx 或其他已配置受信 TLS 证书的反代对外提供 HTTPS，例如在节点对应的 HTTPS server 内加入：

```nginx
location / {
    proxy_pass http://127.0.0.1:8094;
    proxy_http_version 1.1;
    proxy_read_timeout 45s;
}
```

反代保留 `Authorization` 和 `X-Office-Node-ID` 请求头。通过可信内网直连时，可以将 `OFFICE_TOOLS_BIND_ADDRESS` 改为指定的内网接口地址。

## 调并发和维护

在管理端编辑节点的「并发上限」。Sub2API 保存并下发到节点，节点在线应用设置。调低时已开始的任务继续完成，新任务按新的上限派发。连接恢复后，Sub2API 会自动同步已保存的设置。

容器 CPU、内存和临时空间仍由部署配置管理，可结合实际文档的处理负载调整。升级早期版本时先更新办公节点镜像，再通过管理端调整并发。同一台服务重建容器时复用原有 `state` 卷，以保留节点身份和可恢复的结果。

维护时在管理端停止接单，等状态显示已排空，再重建或关停容器。使用全新卷替换机器时，登记为新节点。新增节点使用一份独立持久卷和新令牌。
