"""Client for the isolated executor; it never executes generated code locally."""
from __future__ import annotations

import asyncio
import json

import httpx

from .contracts import RuntimeFailure
from .control import ControlClient, operation_id, validate_origin
from .workspace import VirtualWorkspace

MAX_EXECUTOR_SNAPSHOT = 24 * 1024 * 1024
# One base64 snapshot (outputs is metadata only) plus bounded logs/JSON/path
# overhead. Keep this synchronized with the sibling executor's wire ceiling.
MAX_EXECUTOR_RESPONSE = 4 * ((MAX_EXECUTOR_SNAPSHOT + 2) // 3) + 8 * 1024 * 1024


class OfficeExecutorClient:
    def __init__(self, origin: str, token: str, client: httpx.AsyncClient,
                 control: ControlClient, workspace: VirtualWorkspace, *, journal=True, resources=None):
        self.origin, self.token, self.client = validate_origin(origin), token, client
        self.control, self.workspace, self.journal = control, workspace, journal
        self.resources = resources
        if not self.origin or not token:
            raise ValueError("isolated executor requires a configured origin and token")

    async def execute(self, code: str, call_id: str):
        if not code.strip() or len(code.encode()) > 256 * 1024:
            raise RuntimeFailure("INVALID_SCRIPT", "办公脚本为空或超过试验大小限制")
        manifest = self.control.manifest
        payload = {"version": 1, "run_id": str(manifest.run_id), "lease_epoch": manifest.lease_epoch,
                   "script": code, "inputs": self.workspace.executor_inputs(),
                   "limits": {"timeout_seconds": min(180, manifest.limits.max_run_seconds)}}
        # Lease epochs fence dispatch, but do not rename a graph operation when
        # a different worker resumes its completed result.
        identity = {key: value for key, value in payload.items() if key != "lease_epoch"}
        op = operation_id(str(manifest.run_id), "office", call_id, identity)
        payload["operation_id"] = op
        if self.journal:
            reservation = await self.control.post(self.control.run_path(f"tools/{op}/prepare"),
                                                  {"input": {"script_sha256": operation_id("", "script", "", code),
                                                             "input_files": [{"path": f["path"], "sha256": f["sha256"]} for f in payload["inputs"]]}},
                                                  operation=op)
            if not reservation or not reservation.get("execute"):
                stored = reservation.get("result") if isinstance(reservation, dict) else None
                result = stored.get("output") if isinstance(stored, dict) else None
                if isinstance(result, dict) and result.get("status") in {"timed_out", "output_limit", "invalid_output"}:
                    self._check_outcome(result["status"])
                if (not self.resources or not isinstance(result, dict)
                        or result.get("status") not in {"completed", "failed"}
                        or type(result.get("workspace_version")) is not int or result["workspace_version"] < 0):
                    raise RuntimeFailure("TOOL_REPLAY_REQUIRES_RESOURCE_RESTORE", "已执行步骤的文件恢复信息不完整；不会重复执行", uncertain=True)
                await self.resources.restore(version=result["workspace_version"])
                return result
        try:
            async with self.client.stream("POST", f"{self.origin}/v1/runs/{manifest.run_id}",
                                          json=payload, headers={"Authorization": "Bearer " + self.token,
                                                                 "X-Office-Operation-ID": op},
                                          follow_redirects=False) as response:
                chunks, size = [], 0
                async for chunk in response.aiter_bytes():
                    size += len(chunk)
                    if size > MAX_EXECUTOR_RESPONSE:
                        raise RuntimeFailure("EXECUTOR_RESULT_TOO_LARGE", "办公执行结果超过试验上限", uncertain=True)
                    chunks.append(chunk)
                if response.status_code >= 300:
                    raise RuntimeFailure("EXECUTOR_REJECTED", "隔离执行器拒绝本次操作；请检查运行环境或任务记录", uncertain=response.status_code >= 500 or response.status_code == 409)
                data = json.loads(b"".join(chunks))
            if (data.get("run_id") != str(manifest.run_id) or data.get("operation_id") != op
                    or data.get("lease_epoch") != manifest.lease_epoch or data.get("version") != 1
                    or not isinstance(data.get("workspace_manifest"), list)):
                raise ValueError("invalid executor envelope")
            status = data.get("status")
            if status in {"unknown", "cancelled"}:
                raise RuntimeFailure("EXECUTOR_RESULT_UNKNOWN" if status == "unknown" else "EXECUTOR_CANCELLED",
                                     "办公执行结果未确认或已取消；不会继续生成或自动重试", uncertain=True)
            if status not in {"completed", "failed", "timed_out", "output_limit", "invalid_output"}:
                raise ValueError("invalid executor status")
            if status in {"completed", "failed"}:
                self.workspace.replace_executor_snapshot(data["workspace_manifest"])
            result = {"status": data["status"], "exit_code": data.get("exit_code"),
                      "stdout": data.get("stdout", "")[:65536], "stderr": data.get("stderr", "")[:65536],
                      "files": [{k: row[k] for k in ("path", "size", "sha256", "mime")} for row in data.get("outputs", [])]}
            if self.resources and status in {"completed", "failed"}:
                # Bytes become durable before the operation inbox says complete.
                result["workspace_version"] = await self.resources.persist()
            if self.journal:
                await self.control.post(self.control.run_path(f"tools/{op}/result"),
                                        {"output": result, "http_status": 200}, operation=op, uncertain_on_transport=True)
            self._check_outcome(status)
            return result
        except asyncio.CancelledError:
            await asyncio.shield(self.cancel(op))
            raise
        except (httpx.HTTPError, OSError):
            raise RuntimeFailure("EXECUTOR_RESULT_UNKNOWN", "办公执行连接中断，结果未确认；不会重复执行", uncertain=True) from None
        except (ValueError, TypeError, KeyError):
            raise RuntimeFailure("INVALID_EXECUTOR_RESULT", "办公执行结果校验失败；不会重复执行", uncertain=True) from None

    @staticmethod
    def _check_outcome(status):
        codes = {"timed_out": ("EXECUTOR_TIMED_OUT", "办公执行超时，任务已停止"),
                 "output_limit": ("EXECUTOR_OUTPUT_LIMIT", "办公输出超过限额，任务已停止"),
                 "invalid_output": ("INVALID_EXECUTOR_OUTPUT", "办公输出未通过校验，任务已停止")}
        if status in codes:
            raise RuntimeFailure(*codes[status])

    async def cancel(self, op: str):
        # Best effort; control-plane cancellation still fences late publications.
        try:
            await self.client.delete(f"{self.origin}/v1/runs/{self.control.manifest.run_id}/operations/{op}",
                                     headers={"Authorization": "Bearer " + self.token, "X-Office-Operation-ID": op},
                                     timeout=5, follow_redirects=False)
        except httpx.HTTPError:
            pass
