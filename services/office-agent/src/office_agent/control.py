"""Only the configured Go origin is reachable through this authenticated client."""
from __future__ import annotations

import hashlib
import json
import re
from urllib.parse import urlsplit

import httpx

from .contracts import RunManifest, RuntimeFailure

MAX_RESPONSE_BYTES = 1024 * 1024


def validate_origin(origin: str) -> str:
    parts = urlsplit(origin)
    if (parts.scheme not in {"http", "https"} or not parts.netloc or parts.username or parts.password
            or parts.query or parts.fragment or parts.path not in {"", "/"}):
        raise ValueError("internal service URL must be an origin without credentials, path, query or fragment")
    return origin.rstrip("/")


def unwrap(data):
    return data["data"] if isinstance(data, dict) and "data" in data and "code" in data else data


def operation_id(run_id: str, kind: str, step: str, payload: object) -> str:
    """Stable over a replay of the same graph step, not a new random SDK request."""
    canonical = json.dumps([run_id, kind, step, payload], sort_keys=True,
                           ensure_ascii=False, separators=(",", ":"))
    return f"{kind}-{hashlib.sha256(canonical.encode()).hexdigest()}"


class ControlClient:
    def __init__(self, origin: str, manifest: RunManifest, client: httpx.AsyncClient):
        self.origin = validate_origin(origin)
        self.manifest = manifest
        self.client = client
        self.token = manifest.lease_token.get_secret_value()

    def headers(self, operation: str | None = None) -> dict[str, str]:
        headers = {"Authorization": "Bearer " + self.token,
                   "X-Office-Lease": str(self.manifest.lease_epoch)}
        if operation:
            headers["X-Office-Operation-ID"] = operation
        return headers

    async def post(self, path: str, data: dict, *, operation: str | None = None,
                   uncertain_on_transport: bool = False):
        prefix = f"/internal/office-agent/v1/runs/{self.manifest.run_id}/"
        if not path.startswith(prefix) or ".." in path or "?" in path or "#" in path:
            raise RuntimeFailure("INVALID_CONTROL_PATH", "内部任务地址无效，尚未发起调用")
        try:
            # stream() bounds receipt before allocating a response body. No redirects/retries.
            async with self.client.stream("POST", self.origin + path, json=data,
                                          headers=self.headers(operation), follow_redirects=False) as response:
                chunks, size = [], 0
                async for chunk in response.aiter_bytes():
                    size += len(chunk)
                    if size > MAX_RESPONSE_BYTES:
                        raise RuntimeFailure("RESPONSE_TOO_LARGE", "模型或工具结果超过试验响应上限", uncertain=uncertain_on_transport)
                    chunks.append(chunk)
                raw = b"".join(chunks)
                if response.status_code >= 300:
                    code = "LEASE_LOST" if response.status_code in {401, 403} else "CONTROL_REJECTED"
                    uncertain = uncertain_on_transport and response.status_code >= 500
                    try:
                        error = json.loads(raw).get("error", {})
                        safe_code = error.get("code", "") if isinstance(error, dict) else ""
                        if safe_code in {"NEEDS_CONFIRMATION", "OFFICE_OPERATION_UNKNOWN", "OFFICE_OPERATION_IN_PROGRESS", "OPERATION_UNKNOWN", "OPERATION_DISPATCHED"}:
                            code, uncertain = "OPERATION_UNKNOWN", True
                        elif safe_code in {"LEASE_LOST", "MODEL_ROUTE_CHANGED", "RUN_LIMIT_EXCEEDED", "OFFICE_CAPACITY", "OFFICE_DISABLED"}:
                            code = safe_code
                        elif safe_code == "WORKBENCH_SEARCH_UNAVAILABLE":
                            raise RuntimeFailure(safe_code, "联网搜索暂不可用，请检查工作台搜索供应商", uncertain=uncertain)
                    except (ValueError, AttributeError):
                        pass
                    raise RuntimeFailure(code, "任务调用未完成，请查看任务记录；不会自动重复请求", uncertain=uncertain)
                try:
                    return unwrap(json.loads(raw)) if raw else None
                except ValueError:
                    raise RuntimeFailure("INVALID_CONTROL_RESPONSE", "内部服务返回了无法识别的结果", uncertain=uncertain_on_transport) from None
        except (httpx.HTTPError, OSError):
            raise RuntimeFailure("TRANSPORT_UNKNOWN", "连接中断，本次调用结果尚未确认；不会自动重试", uncertain=uncertain_on_transport) from None

    async def get(self, path: str, *, maximum: int = MAX_RESPONSE_BYTES):
        prefix = self.run_path("")
        base, separator, query = path.partition("?")
        allowed_query = not separator or (base == self.run_path("workspace") and re.fullmatch(r"version=(0|[1-9][0-9]{0,18})", query))
        if not path.startswith(prefix) or any(part in path for part in ("..", "#")) or not allowed_query:
            raise RuntimeFailure("INVALID_CONTROL_PATH", "内部任务地址无效")
        try:
            async with self.client.stream("GET", self.origin + path, headers=self.headers(), follow_redirects=False) as response:
                chunks, size = [], 0
                async for chunk in response.aiter_bytes():
                    size += len(chunk)
                    if size > maximum:
                        raise RuntimeFailure("RESPONSE_TOO_LARGE", "任务资料超过传输上限")
                    chunks.append(chunk)
                if response.status_code >= 300:
                    raise RuntimeFailure("LEASE_LOST" if response.status_code in {401, 403, 409} else "CONTROL_REJECTED", "无法读取任务资料，已停止")
                return unwrap(json.loads(b"".join(chunks)))
        except (httpx.HTTPError, OSError, ValueError):
            raise RuntimeFailure("RESOURCE_TRANSPORT_FAILED", "任务资料读取失败；未重试操作") from None

    async def suspend(self, code: str):
        """Report a stop once; stale/cancelled leases cannot resurrect a run."""
        try:
            async with self.client.stream("POST", self.origin + self.run_path("suspend"),
                                          json={"code": code[:64]}, headers=self.headers(),
                                          follow_redirects=False) as response:
                await self._bounded_body(response, 4096)
                if response.status_code in {401, 409}:
                    return  # Go already fenced this execution or user cancelled.
                if response.status_code != 200:
                    raise RuntimeFailure("SUSPEND_REPORT_FAILED", "任务停止状态尚未确认", uncertain=True)
        except (httpx.HTTPError, OSError):
            raise RuntimeFailure("SUSPEND_REPORT_FAILED", "任务停止状态尚未确认", uncertain=True) from None

    async def model_stream(self, payload: dict, operation: str, *, purpose: str | None = None):
        """Consume the original gateway SSE; Go persists public deltas itself.

        The worker neither republishes token events nor reopens a broken stream.
        JSON responses remain supported for the gateway's durable replay inbox.
        """
        try:
            headers = self.headers(operation)
            if purpose:
                if purpose != "context_compaction":
                    raise RuntimeFailure("INVALID_MODEL_PURPOSE", "模型调用用途无效")
                headers["X-Office-Model-Purpose"] = purpose
            async with self.client.stream("POST", self.origin + self.manifest.bridge_path, json=payload,
                                          headers=headers, follow_redirects=False) as response:
                if response.status_code >= 300:
                    raw = await self._bounded_body(response, MAX_RESPONSE_BYTES)
                    try:
                        error = json.loads(raw).get("error", {})
                        code = error.get("code", "CONTROL_REJECTED")
                    except (ValueError, AttributeError):
                        code = "CONTROL_REJECTED"
                    if response.headers.get("x-office-operation-state") == "completed":
                        # Only Go's completed inbox marker confirms the outcome.
                        # It does not imply whether the ordinary ledger charged.
                        raise RuntimeFailure("MODEL_UPSTREAM_FAILED",
                                             f"模型上游返回 HTTP {response.status_code}，本轮已确认失败；不会自动重试")
                    if response.status_code == 413:
                        raise RuntimeFailure("MODEL_INPUT_TOO_LARGE", "本次模型请求超过网关允许的大小，尚未发送给模型")
                    if code == "BILLING_PENDING":
                        raise RuntimeFailure(code, "上一轮模型已完成，计费记录仍在入账；本步骤尚未调用，任务已暂停", uncertain=True)
                    uncertain = code in {"NEEDS_CONFIRMATION", "OPERATION_UNKNOWN", "OPERATION_DISPATCHED"} or (response.status_code >= 500 and code != "OFFICE_CAPACITY")
                    raise RuntimeFailure("OPERATION_UNKNOWN" if uncertain else code,
                                         "模型调用未完成；不会自动重复请求", uncertain=uncertain)
                if not response.headers.get("content-type", "").lower().startswith("text/event-stream"):
                    raw = await self._bounded_body(response, MAX_RESPONSE_BYTES)
                    yield {"_complete_response": json.loads(raw)}
                    return
                buffer, total, data = "", 0, []
                async for chunk in response.aiter_text():
                    total += len(chunk.encode())
                    if total > MAX_RESPONSE_BYTES:
                        raise RuntimeFailure("RESPONSE_TOO_LARGE", "模型结果超过响应上限", uncertain=True)
                    buffer += chunk
                    while "\n" in buffer:
                        line, buffer = buffer.split("\n", 1)
                        line = line.removesuffix("\r")
                        if not line:
                            if data:
                                event = "\n".join(data)
                                data = []
                                if event == "[DONE]":
                                    return
                                yield json.loads(event)
                        elif line.startswith("data:"):
                            data.append(line[5:].removeprefix(" "))
                raise RuntimeFailure("MODEL_STREAM_INCOMPLETE", "模型响应中断，结果尚未确认；不会重放", uncertain=True)
        except (httpx.HTTPError, OSError):
            raise RuntimeFailure("TRANSPORT_UNKNOWN", "模型连接中断，结果尚未确认；不会自动重试", uncertain=True) from None
        except (ValueError, TypeError):
            raise RuntimeFailure("INVALID_MODEL_STREAM", "模型响应格式无效，结果尚未确认", uncertain=True) from None

    @staticmethod
    async def _bounded_body(response, maximum):
        chunks, size = [], 0
        async for chunk in response.aiter_bytes():
            size += len(chunk)
            if size > maximum:
                raise RuntimeFailure("RESPONSE_TOO_LARGE", "服务响应超过限制", uncertain=True)
            chunks.append(chunk)
        return b"".join(chunks)

    async def heartbeat(self):
        data = await self.post(self.run_path("heartbeat"), {})
        if not isinstance(data, dict) or not isinstance(data.get("lease_token"), str):
            raise RuntimeFailure("INVALID_HEARTBEAT", "任务续约结果无效")
        self.token = data["lease_token"]

    def run_path(self, suffix: str) -> str:
        return f"/internal/office-agent/v1/runs/{self.manifest.run_id}/{suffix}"

    async def progress(self, message: str, event_id: str):
        return await self.post(self.run_path("events"),
                               {"event_id": event_id, "type": "progress", "payload": {"message": message}})

    async def event(self, kind: str, payload: dict, event_id: str):
        return await self.post(self.run_path("events"), {"event_id": event_id, "type": kind, "payload": payload})
