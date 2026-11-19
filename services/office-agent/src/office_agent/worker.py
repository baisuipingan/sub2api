"""Single-claim polling worker. Go alone owns scheduling, leases and billing."""
from __future__ import annotations

import argparse
import asyncio
import json
import os
import socket
import sys
import time
from contextlib import suppress
from uuid import uuid4

import httpx

from .contracts import RunManifest, RunResult, RuntimeFailure
from .control import ControlClient, unwrap, validate_origin
from .executor import OfficeExecutorClient
from .chat import ChatRuntime
from .resources import ResourceClient
from .runtime import OfficeRuntime
from .workspace import VirtualWorkspace
from .diagnostics import report_runtime_exception


async def heartbeat_loop(control: ControlClient):
    while True:
        await asyncio.sleep(20)
        await control.heartbeat()


async def process_manifest(origin: str, manifest: RunManifest, client: httpx.AsyncClient, *,
                           executor_origin: str = "", executor_token: str = "", preset: str | None = None) -> RunResult:
    control = ControlClient(origin, manifest, client)
    if preset is not None and manifest.mode != preset:
        raise RuntimeFailure("WORKER_PRESET_MISMATCH", "任务类型与本 worker 不匹配，未发起模型调用")
    if manifest.mode == "chat":
        runtime = ChatRuntime(control)
    else:
        workspace = VirtualWorkspace(with_skills=bool(executor_origin))
        resources = ResourceClient(control, workspace) if manifest.durable_resume else None
        executor = OfficeExecutorClient(executor_origin, executor_token, client, control, workspace, resources=resources) if executor_origin else None
        runtime = OfficeRuntime(control, executor=executor, workspace=workspace, resources=resources)
    run_task = asyncio.create_task(runtime.execute(control.progress))
    heartbeat = asyncio.create_task(heartbeat_loop(control))
    try:
        done, _ = await asyncio.wait({run_task, heartbeat}, return_when=asyncio.FIRST_COMPLETED)
        if heartbeat in done:
            # Lease loss fences *all* future model/tool calls. Do not finish with
            # an expired credential or let the old graph continue in background.
            run_task.cancel()
            with suppress(asyncio.CancelledError):
                await run_task
            exc = heartbeat.exception()
            if isinstance(exc, RuntimeFailure):
                return exc.result()
            return RunResult(status="needs_confirmation", error_code="LEASE_LOST",
                             error_message="任务租约失效，运行已停止；不会重放未确认操作")
        result = run_task.result()
        if result.status == "needs_confirmation":
            # Immediate Go-owned fencing avoids a stale "running" UI until
            # lease expiry. A lost report still uses Go's conservative recovery.
            # The endpoint ignores an already fenced lease; it cannot resume it.
            with suppress(RuntimeFailure):
                await control.suspend(result.error_code)
        elif result.status != "waiting_user":
            # RunResult also carries clarification for wait/resume. Finish has
            # a separate strict Go contract; even clarification:null is invalid.
            finish = {"status": result.status, "content": result.content,
                      "error_code": result.error_code, "error_message": result.error_message}
            await control.post(control.run_path("finish"), finish, uncertain_on_transport=True)
        return result
    finally:
        for task in (run_task, heartbeat):
            if not task.done():
                task.cancel()
        await asyncio.gather(run_task, heartbeat, return_exceptions=True)


async def claim(client: httpx.AsyncClient, origin: str, token: str, worker_id: str, preset: str = "chat", executor_ready: bool | None = None) -> RunManifest | None:
    if preset not in {"chat", "office"}:
        raise RuntimeFailure("WORKER_PRESET_INVALID", "worker preset 必须为 chat 或 office")
    try:
        async with client.stream("POST", origin.rstrip("/") + "/internal/office-agent/v1/claim",
                                 json={"worker_id": worker_id, "preset": preset, "executor_ready": executor_ready}, headers={"Authorization": "Bearer " + token},
                                 follow_redirects=False) as response:
            if response.status_code == 204:
                return None
            if response.status_code != 200:
                raise RuntimeFailure("CLAIM_REJECTED", "服务端拒绝领取任务，请核对试验开关和 worker 配置")
            chunks, size = [], 0
            async for chunk in response.aiter_bytes():
                size += len(chunk)
                # 2 MiB decoded text can require 6x JSON escaping on the wire.
                if size > 13 * 1024 * 1024:
                    raise RuntimeFailure("MANIFEST_TOO_LARGE", "任务清单超出试验大小限制")
                chunks.append(chunk)
            try:
                return RunManifest.model_validate(unwrap(json.loads(b"".join(chunks))))
            except (ValueError, TypeError):
                raise RuntimeFailure("INVALID_MANIFEST", "任务清单不符合已批准的试验契约") from None
    except httpx.HTTPError:
        raise RuntimeFailure("CLAIM_TRANSPORT_FAILED", "领取任务连接失败；未收到的任务不会在本机执行") from None


async def executor_health(client, origin):
    if not origin:
        return False
    try:
        async with client.stream("GET", validate_origin(origin) + "/healthz", timeout=2, follow_redirects=False) as response:
            raw = await ControlClient._bounded_body(response, 4096)
            data = json.loads(raw)
            return response.status_code == 200 and data.get("status") == "ready" and data.get("execution_runtime") == "runsc"
    except (RuntimeFailure, httpx.HTTPError, OSError, ValueError, AttributeError):
        return False


async def run_worker(*, once=False):
    origin = os.environ.get("OFFICE_CONTROL_ORIGIN", "").rstrip("/")
    token = os.environ.get("OFFICE_WORKER_TOKEN", "")
    if not origin or not token:
        raise RuntimeFailure("WORKER_CONFIG_REQUIRED", "必须配置 OFFICE_CONTROL_ORIGIN 和 OFFICE_WORKER_TOKEN")
    try:
        origin = validate_origin(origin)
    except ValueError:
        raise RuntimeFailure("WORKER_CONFIG_INVALID", "内部控制面必须配置固定 origin，不能包含路径或用户密码") from None
    worker_id = os.environ.get("OFFICE_WORKER_ID") or f"office-{socket.gethostname()[:32]}-{uuid4().hex[:12]}"
    preset = os.environ.get("OFFICE_WORKER_PRESET", "chat")
    if preset not in {"chat", "office"}:
        raise RuntimeFailure("WORKER_PRESET_INVALID", "OFFICE_WORKER_PRESET 必须为 chat 或 office")
    # File execution remains opt-in, and separate from the chat worker pool.
    # Public office enablement still requires actual Linux/gVisor acceptance.
    enabled = os.environ.get("OFFICE_EXECUTOR_PILOT_ENABLED", "false").lower() == "true"
    executor_origin = os.environ.get("OFFICE_EXECUTOR_ORIGIN", "") if enabled and preset == "office" else ""
    executor_token = os.environ.get("OFFICE_EXECUTOR_TOKEN", "") if enabled and preset == "office" else ""
    # Go owns a 300-second model-call deadline. Do not create an earlier 210s
    # read timeout while a legitimate, billed upstream response is still active.
    async with httpx.AsyncClient(timeout=httpx.Timeout(310, connect=5), trust_env=False,
                                 limits=httpx.Limits(max_connections=4, max_keepalive_connections=2)) as client:
        health_checked, executor_ready = 0.0, None
        while True:
            if preset == "office" and time.monotonic() - health_checked >= 30:
                executor_ready = await executor_health(client, executor_origin)
                health_checked = time.monotonic()
            manifest = await claim(client, origin, token, worker_id, preset, executor_ready)
            if manifest:
                result = await process_manifest(origin, manifest, client,
                                                executor_origin=executor_origin, executor_token=executor_token, preset=preset)
                print(json.dumps({"run_id": str(manifest.run_id), "status": result.status,
                                  "error_code": result.error_code}), flush=True)
            if once:
                return
            if manifest is None:
                await asyncio.sleep(5)


def main():
    parser = argparse.ArgumentParser(description="Internal Sub2API chat/office worker (no public server)")
    parser.add_argument("--once", action="store_true", help="claim at most one task, useful for integration tests")
    args = parser.parse_args()
    # Explicitly ignore environment-provided external tracing keys.
    os.environ["LANGSMITH_TRACING"] = "false"
    os.environ["LANGCHAIN_TRACING_V2"] = "false"
    try:
        asyncio.run(run_worker(once=args.once))
    except RuntimeFailure as exc:
        print(json.dumps({"error_code": exc.code, "message": exc.message}), file=sys.stderr)
        raise SystemExit(1) from None
    except Exception as exc:
        report_runtime_exception("worker", None, exc, stage="poll")
        print(json.dumps({"error_code": "WORKER_INTERNAL_ERROR",
                          "message": "试验 worker 异常退出，请检查内部运行记录；不会自行重放任务"}), file=sys.stderr)
        raise SystemExit(1) from None
    except KeyboardInterrupt:
        raise SystemExit(130) from None


if __name__ == "__main__":
    main()
