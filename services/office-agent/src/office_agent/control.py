"""Only the configured Go origin is reachable through this authenticated client."""
from __future__ import annotations

import hashlib
import json
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
                    uncertain = False
                    try:
                        error = json.loads(raw).get("error", {})
                        safe_code = error.get("code", "") if isinstance(error, dict) else ""
                        if safe_code in {"NEEDS_CONFIRMATION", "OFFICE_OPERATION_UNKNOWN", "OFFICE_OPERATION_IN_PROGRESS", "OPERATION_UNKNOWN", "OPERATION_DISPATCHED"}:
                            code, uncertain = "OPERATION_UNKNOWN", True
                        elif safe_code in {"LEASE_LOST", "MODEL_ROUTE_CHANGED", "RUN_LIMIT_EXCEEDED", "OFFICE_CAPACITY", "OFFICE_DISABLED"}:
                            code = safe_code
                    except (ValueError, AttributeError):
                        pass
                    raise RuntimeFailure(code, "任务调用未完成，请查看任务记录；不会自动重复请求", uncertain=uncertain)
                try:
                    return unwrap(json.loads(raw)) if raw else None
                except ValueError:
                    raise RuntimeFailure("INVALID_CONTROL_RESPONSE", "内部服务返回了无法识别的结果", uncertain=uncertain_on_transport) from None
        except (httpx.HTTPError, OSError):
            raise RuntimeFailure("TRANSPORT_UNKNOWN", "连接中断，本次调用结果尚未确认；不会自动重试", uncertain=uncertain_on_transport) from None

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
