# BEpusdt 充值

Sub2API 直接调用 BEpusdt v1.24.4 的原生接口。站内订单以 CNY 计价，收银台显示应付的 BSC USDT。余额额度由 Sub2API 的充值配置计算。

## 收银台和管理端

通过 `SUB2API_DOCKER_NETWORK` 指定主站 Docker 网络，再运行 `docker compose up -d --wait`。1Panel 将支付域名反代到 `http://127.0.0.1:18090`，启用 HTTPS，保留请求的 Host。收银台代理只开放收银台、静态资源及查单。管理端通过 SSH 隧道连接服务器的 `127.0.0.1:18091`。

在 BEpusdt 管理端配置 `usdt.bep20` 收款地址、BSC RPC 和收银台域名。API 令牌填到 Sub2API 的支付渠道里。

## Sub2API 支付设置

添加 BEpusdt 实例，选择 USDT (BEP20)，API 地址填 `http://milo-bepusdt:8080`，收银台域名填支付域名的 HTTPS origin。通知地址使用主站的 `/api/v1/payment/webhook/bepusdt`，回跳由订单携带。启用前用测试钱包验证下单、查单、取消及真实小额收款后的余额入账和重复回调。
