# Office Executor — 阶段 0 监督组件

该目录只负责把一段办公 Python 和明确授权的文件快照放进**一次性 Linux/gVisor 任务容器**，返回受限文件快照。会话、模型调用、计费、S3/R2 归档由 Go 控制面和 `services/office-agent` 负责。监督进程从不在宿主执行模型脚本。

当前是可测试的运行监督组件和部署契约，**尚未通过真实 Linux/gVisor 的 Office 端到端验收**。2026-11-18 只读检查本机 OrbStack，仅有 runc，无 runsc；本轮未拉取/构建大型镜像，未安装运行时，也未部署。缺少 Linux、runsc 或预装固定镜像均直接失败，不退回 runc 或宿主 Python。

## 边界

- 可信监督器持有私有 HTTP token 和 Docker socket。它不是给浏览器/用户直接调用的 API，不应暴露公网；推荐专用算力机与主站隔离。
- 不可信办公容器不接触 socket、模型 Key、对象存储凭据、数据库或控制面网络。容器固定 `runsc`、`network=none`、非 root、capabilities 全移除、no-new-privileges、只读根文件系统；不能由请求选择镜像、命令、环境、挂载或运行时。
- 只有 `uploads/`（只读输入）、`output/`、`scratch/` 及镜像内只读 `skills/`。工作盘是受大小限制的 tmpfs，计入容器内存上限，不是无配额宿主目录；`/tmp` 另限 32 MiB。任务结束强制删除容器，临时挂载随之消失。
- 模型脚本能使用预装 Python 和 Office 二进制；这不等于 Python 语言内已禁止系统调用。安全边界是任务沙箱、无网络/无凭据和资源限额，不是 AST 黑名单。产品不提供交互终端或安装依赖接口。
- 默认每任务 180 秒、2 GiB 内存、1 CPU、256 MiB 工作盘、128 PID、24 MiB 输出快照、64 文件、stdout/stderr 各 64 KiB。请求仅能下调。默认监督器同时两任务；CPU/内存应按全部并发任务另加监督器本身的开销预算。
- 宿主短期 staging（最多 24 MiB 输入/任务）在 `finally` 清理。结果默认缓存一小时，定时清理 payload，保留小型操作 tombstone 防重放；总状态空间默认 1 GiB，剩余磁盘低于 256 MiB 时拒绝新任务。

## HTTP 契约 v1

`POST /v1/runs/{run_id}`。须同时提供：

```http
Authorization: Bearer <独立的 executor token，不是模型 API key>
Content-Type: application/json
X-Office-Operation-ID: op_opaque_123
```

```json
{
  "version": 1,
  "run_id": "run_opaque_123",
  "operation_id": "op_opaque_123",
  "lease_epoch": 1,
  "script": "from pathlib import Path\nPath('output/result.txt').write_text('done')",
  "inputs": [
    {
      "path": "uploads/source.txt",
      "content_base64": "aGVsbG8=",
      "sha256": "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
    }
  ],
  "limits": {"timeout_seconds": 120}
}
```

请求体最多 34 MiB，解码后输入总量最多 24 MiB/64 文件。`run_id`、`operation_id` 仅允许不超过 96 字符的字母、数字、`_`、`-`。输入路径允许 `uploads/`、`output/`、`scratch/`，后两者用于恢复上次执行快照；不接受绝对路径、`..`、规范化歧义和文件/目录冲突。所有文件必须校验 SHA-256。

正常返回 HTTP 200，包含 `version/run_id/operation_id/lease_epoch/status/exit_code/stdout/stderr/duration_ms` 及：

```json
{
  "workspace_manifest": [
    {"path": "output/result.txt", "size": 4, "sha256": "...", "mime": "text/plain", "content_base64": "ZG9uZQ=="}
  ],
  "outputs": [
    {"path": "output/result.txt", "size": 4, "sha256": "...", "mime": "text/plain"}
  ]
}
```

`workspace_manifest` 是 `output/` 和 `scratch/` 的**完整写区快照**，不是增量补丁。调用方应替换写区快照，保留只读 uploads；manifest 未出现的旧文件视为已删除。`scratch/.home`、`.tmp`、`.cache`、`.lo-profile` 是运行缓存，永不回灌。`outputs` 仅含 `output/` 子集的路径、大小、摘要和 MIME **元数据，不重复 base64**；成品内容按 path 从 `workspace_manifest` 获取。全响应上限 40 MiB，与 office-agent client 一致；24 MiB 二进制只编码一次。非完成状态不应自动向用户发布半成品。

状态：`completed`、`failed`、`timed_out`、`cancelled`、`output_limit`、`invalid_output`、`unknown`。只有已验证 runner 返回的 `completed/failed` 包含可替换虚拟写区的完整快照；容器 OOM 或入口崩溃而未返回有效 runner 结果时为 `unknown`，不能用空 manifest 清除上次工作区。`unknown` 表示监督器无法证明最终结果，调用方必须停止自动重放并交由控制面恢复。退出日志只供受信控制面处理，不能不经筛选全部直传用户。

取消：`DELETE /v1/runs/{run_id}/operations/{operation_id}`，相同 Bearer token 和 operation header；202 只表示已接受取消信号，最终停止状态从原 POST 返回。未运行返回 404。浏览器取消不能直接跳过 Go 的权限检查。

幂等：相同 `(run_id, operation_id)` 和完整参数摘要返回已存结果；更换参数返回 409。执行中、重启结果未知、结果 payload 已过期均不重新执行。`lease_epoch` 只允许单调前进；每 run 同时一个工具操作。监督器使用 SQLite 本地 journal 和目录单实例锁；控制面的全链路租约及模型调用幂等仍需独立实现。

## 输出检查

两层检查（容器 runner 与可信监督器）都限制路径、文件数、总字节与校验和。只读取普通文件，拒绝符号链接、父目录链接、硬链接与特殊设备/FIFO；检查读取期间文件未变。`output/` 仅接受 DOCX/XLSX/PPTX/PDF/PNG/JPEG/WebP/UTF-8 TXT/Markdown/CSV/JSON，校验基本签名、Office 结构和压缩包上限，拒绝宏和越界 archive 条目。HTML/SVG 不作为可发布输出。

这些是文件边界校验，不是完整恶意文档扫描或 Office 语义正确性证明。正式接入仍需上传校验、存储 ACL、不可执行附件下载、安全预览及文档验证流程。`scratch/` 可以保存工作脚本和二进制恢复状态，控制面不得将它当成公开附件目录。

## 预装依赖与源技能

`Dockerfile` 安装固定版本 Python Office 依赖、LibreOffice Writer/Calc/Impress、Poppler、pandoc、Noto CJK 字体。只从 `services/office-agent/vendor/bisheng/skills` 复制已经审计的三套真实技能及模板，不从远程运行时安装技能。PyMuPDF 不在默认依赖中；渲染脚本使用 Poppler `pdfinfo` / `pdftoppm`，修改记录保存在 vendor 来源清单。

`requirements-task.txt` 固定直接及已知传递依赖；基镜像必须由构建参数提供摘要。发行前应对目标 Linux 架构解析并验证依赖、生成 SBOM 和镜像扫描结果，记录 apt 包版本，随后以最终镜像摘要运行。当前尚未做镜像构建，不能把已写 Dockerfile 当作已通过二进制兼容验证。第三方 Office、字体、模板和 Python 库分别遵守其原始许可证。

## 本地验证

无 Docker/无需大依赖的单元与 HTTP 测试（不执行办公脚本）：

```sh
cd services/office-executor
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -v
```

覆盖路径/签名/压缩包/链接/大小/文件数、固定容器安全参数、认证与 operation 绑定、重复请求/参数冲突/旧租约、重启未知结果、结果过期与 fail-closed。测试使用假的 Docker 执行结果，不能代替真实隔离测试。

只有预配置 Linux/gVisor 的算力机才能执行：

```sh
export OFFICE_EXECUTOR_IMAGE='registry.example/office-task@sha256:<reviewed-image-digest>'
export OFFICE_EXECUTOR_STATE_DIR='/var/lib/milo-office-executor'
PYTHONDONTWRITEBYTECODE=1 python3 -m office_executor doctor
PYTHONDONTWRITEBYTECODE=1 python3 tests/linux_acceptance.py --allow-task-execution
```

真实验收会运行隔离探针、无网络/无密钥/只读输入检查、三种 Office 文件生成、**真实 vendor 技能检查/公式重算/DOCX 与 PPTX 渲染**、符号链接拒绝、超时、输出与工作盘上限。它会创建一次性任务容器；不能与运行中的服务共用状态目录（单实例锁会阻止）。尚待补充压力/故障注入测试：PID 与内存极限、并发取消、Docker daemon 重启及满盘恢复，以及版式视觉验收。

## 运行

CLI 支持 `doctor`、从 stdin 读同协议 JSON 的 `execute`、`serve`。配置：

| 变量 | 含义 |
| --- | --- |
| `OFFICE_EXECUTOR_IMAGE` | 必填，不可变 `repository@sha256:...` 或本地 `sha256:...` image ID |
| `OFFICE_EXECUTOR_STATE_DIR` | 默认 `/var/lib/milo-office-executor`，独立持久目录 |
| `OFFICE_EXECUTOR_CONCURRENCY` | 默认 2，1–8，需结合算力预算 |
| `OFFICE_EXECUTOR_TOKEN_FILE` | 推荐：独立随机 token 文件 |
| `OFFICE_EXECUTOR_TOKEN` | 可选：独立 token；不得复用模型/主站 token |
| `OFFICE_EXECUTOR_HOST/PORT` | 默认 `127.0.0.1:8093`，只提供给可信 office-agent |

独立部署示例在 `deploy/office-executor/compose.yaml`，默认不修改主站部署。监督器 socket 等价高权限能力，应只部署可信发行镜像；主站不挂这个 socket。监督器容器与宿主必须使用同一绝对 staging 目录，不能用路径不一致的 named volume。

停止时取消活动任务；重启后把未完成 journal 标为未知，清理本实例 namespace 的遗留容器和 staging，禁止自动重放。清理失败则拒绝继续启动。对象存储上传/下载不发生在不可信任务容器中，而由持有资源权限的控制面按已批准资源 ID 完成。
