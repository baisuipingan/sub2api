# MiloCode 模型能力目录

本分支只托管经审核的模型规格数据，不包含服务源码或可执行配置。稳定地址：

https://raw.githubusercontent.com/baisuipingan/sub2api/model-profiles/desktop-agent-profiles.json

## 规格语义

- `official_context_window`、`max_input_tokens`、`official_max_output_tokens` 记录厂商明确公布的原始规格。未单独公布的值为 `null`，不通过上下文减输出推算。
- `output_limit_kind` 为省略/空/`fixed` 时，`model.max_output_tokens` 是正数已公布最大输出；`unlimited` 或 `not_published` 时以 `0` 表示无独立上限或未公布，不能当作请求预算。
- `model.default_max_output_tokens` 是正数运营预算，与官方最大值分开。Gemini 4 百万输出有 long decode 条件，目录记录其官方最大值，不表示当前同步执行链已经实现长输出。
- 每项保留厂商官方来源、官方推理档位和现有执行链可表达的能力；目录不包含账户、Key、计费分组或远程可执行映射。

## 维护

更新前核对每项 `source_url` 指向的厂商官方文档，更新 `revision`，保持 ID/别名所有权与向后兼容，并使用 sub2api 的目录校验/回归测试。服务仓库的导出命令为：

```sh
cd backend
go run ./cmd/export-desktop-agent-profiles -output ../output/desktop-agent-profiles-official.json
```

将审核后的导出文件更新到本分支根目录即可；维护本数据分支不需要发布服务版本、打 tag、构建镜像或打包客户端。服务端在启动、定时或管理员手动刷新时拉取目录，校验失败保留最后有效目录；新模型和不兼容协议能力仍由服务端合同限制。
