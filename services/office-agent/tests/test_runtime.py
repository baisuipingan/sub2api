import asyncio
import hashlib
import json
from datetime import datetime, timedelta, timezone
from uuid import uuid4

import httpx
import pytest

from office_agent.contracts import RunManifest, RuntimeFailure
from office_agent.control import ControlClient, operation_id
from office_agent.runtime import OfficeRuntime, SerialToolBudget
from office_agent.workspace import VirtualWorkspace, logical_path, vendor_root
from office_agent.worker import process_manifest


def manifest(**overrides):
    run, conversation = str(uuid4()), str(uuid4())
    data = {"run_id": run, "conversation_id": conversation, "thread_id": conversation,
            "lease_epoch": 1, "lease_token": "secret-test-lease",
            "lease_until": datetime.now(timezone.utc) + timedelta(seconds=90),
            "model": "test-public-alias", "bridge_path": f"/internal/office-agent/v1/runs/{run}/model/chat/completions",
            "limits": {"max_model_calls": 4, "max_tool_calls": 8, "max_output_tokens": 1024, "max_run_seconds": 30},
            "messages": [{"role": "user", "content": "你好"}], "input_resources": None}
    data.update(overrides)
    return RunManifest.model_validate(data)


@pytest.mark.asyncio
async def test_real_deepagents_graph_uses_only_go_bridge_and_safe_tools():
    seen = []
    async def respond(request):
        body = json.loads(request.content)
        seen.append((request, body))
        assert request.headers["authorization"] == "Bearer secret-test-lease"
        assert request.headers["x-office-lease"] == "1"
        assert request.headers["x-office-operation-id"].startswith("model-")
        assert not ({"execute", "task", "delete_file", "delete"} & {t["function"]["name"] for t in body.get("tools", [])})
        return httpx.Response(200, json={"choices": [{"message": {"role": "assistant", "content": "你好，测试完成"}, "finish_reason": "stop"}],
                                         "usage": {"prompt_tokens": 10, "completion_tokens": 4, "total_tokens": 14}})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        control = ControlClient("https://control.test", manifest(), client)
        runtime = OfficeRuntime(control)
        result = await runtime.execute()
    assert result.status == "completed", result
    assert result.content == "你好，测试完成"
    assert len(seen) == 1
    assert "secret-test-lease" not in repr(runtime.graph)
    assert "secret-test-lease" not in repr(control.manifest)


@pytest.mark.asyncio
async def test_unknown_model_result_is_not_automatically_retried():
    calls = 0
    async def respond(request):
        nonlocal calls
        calls += 1
        raise httpx.ReadTimeout("token-must-never-escape")
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        result = await OfficeRuntime(ControlClient("https://control.test", manifest(), client)).execute()
    assert calls == 1
    assert result.status == "needs_confirmation"
    assert result.error_code == "TRANSPORT_UNKNOWN"
    assert "token-must-never-escape" not in result.model_dump_json()


def test_manifest_cannot_redirect_model_credential():
    with pytest.raises(ValueError):
        manifest(bridge_path="https://attacker.test/v1/chat/completions")
    safe = manifest()
    assert "secret-test-lease" not in safe.model_dump_json()
    with pytest.raises(ValueError):
        ControlClient("https://user:secret@control.test", safe, None)


def test_workspace_is_virtual_read_only_skills_and_rejects_traversal():
    ws = VirtualWorkspace()
    assert ws.read("/skills/bisheng-docx/SKILL.md").error is None
    assert ws.write("/skills/bisheng-docx/SKILL.md", "bad").error
    assert ws.write("/uploads/a.txt", "bad").error
    for path in ["/etc/passwd", "/scratch/../../etc/passwd", "/scratch//x", "/scratch/a\\b", "C:/tmp/x"]:
        assert ws.write(path, "bad").error
    assert ws.write("/scratch/a.py", "print('中文')").error is None
    assert ws.read("/scratch/a.py").file_data["content"] == "print('中文')"


def test_vendor_sources_integrity_manifest():
    source = json.loads((vendor_root() / "SOURCE_MANIFEST.json").read_text())
    assert source["commit"] == "a8e5be89c1b8d3a2e512c772e2a38de409336dc8"
    for row in source["files"]:
        assert hashlib.sha256((vendor_root() / row["local_path"]).read_bytes()).hexdigest() == row.get("local_sha256", row["sha256"])


@pytest.mark.asyncio
async def test_tool_budget_serializes_parallel_calls():
    middleware, active, peak = SerialToolBudget(2), 0, 0
    async def handler(request):
        nonlocal active, peak
        active += 1
        peak = max(peak, active)
        await asyncio.sleep(0.01)
        active -= 1
        return "ok"
    assert await asyncio.gather(middleware.awrap_tool_call(None, handler), middleware.awrap_tool_call(None, handler)) == ["ok", "ok"]
    assert peak == 1
    with pytest.raises(RuntimeFailure):
        await middleware.awrap_tool_call(None, handler)


@pytest.mark.asyncio
async def test_no_false_durable_resume_or_attachment_support():
    async with httpx.AsyncClient() as client:
        for override, code in [({"durable_resume": True}, "DURABLE_RESUME_NOT_READY"),
                               ({"input_resources": [{"resource_id": "r", "version_id": "v"}]}, "INPUT_RESOURCES_NOT_READY")]:
            result = await OfficeRuntime(ControlClient("https://control.test", manifest(**override), client)).execute()
            assert result.status == "failed"
            assert result.error_code == code


def test_operation_identity_stable_and_payload_sensitive():
    assert operation_id("r", "model", "step1", {"a": 1}) == operation_id("r", "model", "step1", {"a": 1})
    assert operation_id("r", "model", "step1", {"a": 1}) != operation_id("r", "model", "step2", {"a": 1})


@pytest.mark.asyncio
async def test_model_operation_identity_stable_across_rebuilds():
    identifiers = []
    async def respond(request):
        identifiers.append(request.headers["x-office-operation-id"])
        return httpx.Response(200, json={"choices": [{"message": {"role": "assistant", "content": "ok"}}]})
    item = manifest()
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        for _ in range(2):
            assert (await OfficeRuntime(ControlClient("https://control.test", item, client)).execute()).status == "completed"
    assert identifiers[0] == identifiers[1]


@pytest.mark.asyncio
async def test_real_graph_office_tool_is_journaled_and_snapshot_restored():
    import base64
    from office_agent.executor import OfficeExecutorClient
    calls, model_calls = [], 0
    content = b"stage-zero document bytes"
    output = {"path": "output/report.docx", "size": len(content), "sha256": hashlib.sha256(content).hexdigest(),
              "mime": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
              "content_base64": base64.b64encode(content).decode()}
    async def respond(request):
        nonlocal model_calls
        calls.append(request.url.path)
        body = json.loads(request.content)
        if request.url.path.endswith("chat/completions"):
            model_calls += 1
            names = {x["function"]["name"] for x in body.get("tools", [])}
            assert "execute_office_python" in names
            assert "task" not in names and "execute" not in names
            message = {"role": "assistant", "content": "完成试验验证"} if model_calls == 2 else {
                "role": "assistant", "content": "", "tool_calls": [{"id": "call-office-1", "type": "function",
                    "function": {"name": "execute_office_python", "arguments": json.dumps({"python_code": "print('validated')"})}}]}
            return httpx.Response(200, json={"choices": [{"message": message}]})
        if request.url.path.endswith("/prepare"):
            return httpx.Response(200, json={"state": "dispatched", "execute": True})
        if request.url.path.endswith("/result"):
            assert "content_base64" not in request.content.decode()
            return httpx.Response(200, json={"state": "completed"})
        assert request.url.host == "executor.test"
        assert request.headers["authorization"] == "Bearer isolated-executor-only"
        assert body["operation_id"] == request.headers["x-office-operation-id"]
        assert body["lease_epoch"] == 1
        return httpx.Response(200, json={"version": 1, "run_id": str(item.run_id), "operation_id": body["operation_id"], "lease_epoch": 1, "status": "completed", "exit_code": 0,
                                         "stdout": "validated", "stderr": "", "workspace_manifest": [output], "outputs": [output]})
    item = manifest()
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        control = ControlClient("https://control.test", item, client)
        ws = VirtualWorkspace()
        executor = OfficeExecutorClient("https://executor.test", "isolated-executor-only", client, control, ws)
        result = await OfficeRuntime(control, executor=executor, workspace=ws).execute()
    assert result.status == "completed", result
    assert model_calls == 2
    assert ws.files["output/report.docx"] == content
    assert sum(path.endswith("/prepare") for path in calls) == 1
    assert sum(path.endswith("/result") for path in calls) == 1


@pytest.mark.asyncio
async def test_executor_unknown_is_not_retried_and_graph_stops():
    from office_agent.executor import OfficeExecutorClient
    count = 0
    async def respond(request):
        nonlocal count
        if request.url.path.endswith("/prepare"):
            return httpx.Response(200, json={"execute": True})
        count += 1
        raise httpx.ReadTimeout("uncertain")
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        control = ControlClient("https://control.test", manifest(), client)
        executor = OfficeExecutorClient("https://executor.test", "key", client, control, VirtualWorkspace())
        with pytest.raises(RuntimeFailure) as failure:
            await executor.execute("print('x')", "call-1")
    assert count == 1
    assert failure.value.uncertain


@pytest.mark.asyncio
async def test_poll_worker_finishes_safe_final_content():
    paths = []
    async def respond(request):
        paths.append(request.url.path)
        if request.url.path.endswith("chat/completions"):
            return httpx.Response(200, json={"choices": [{"message": {"role": "assistant", "content": "pilot completed"}}]})
        if request.url.path.endswith("/events"):
            assert json.loads(request.content)["type"] == "progress"
        if request.url.path.endswith("/finish"):
            assert json.loads(request.content)["status"] == "completed"
        return httpx.Response(200, json={"code": 0, "data": {}})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        result = await process_manifest("https://control.test", manifest(), client)
    assert result.status == "completed"
    assert paths[-1].endswith("/finish")


@pytest.mark.asyncio
async def test_go_needs_confirmation_survives_error_translation():
    async def respond(request):
        return httpx.Response(409, json={"error": {"code": "NEEDS_CONFIRMATION", "message": "结果未知", "needs_confirmation": True}})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        result = await OfficeRuntime(ControlClient("https://control.test", manifest(), client)).execute()
    assert result.status == "needs_confirmation"
    assert result.error_code == "OPERATION_UNKNOWN"


def test_snapshot_replacement_is_atomic_and_keeps_uploads_read_only():
    import base64
    ws = VirtualWorkspace(with_skills=False)
    assert ws.write("/scratch/keep.txt", "original").error is None
    bad = {"path": "output/x.txt", "size": 3, "sha256": "0" * 64, "content_base64": base64.b64encode(b"bad").decode()}
    with pytest.raises(ValueError):
        ws.replace_executor_snapshot([bad])
    assert ws.files["scratch/keep.txt"] == b"original"
    with pytest.raises(ValueError):
        ws.replace_executor_snapshot([{**bad, "path": "uploads/injected.txt"}])
    assert ws.download_files(["/etc/passwd"])[0].error == "permission_denied"


def test_poppler_render_helpers_do_not_import_pymupdf():
    import ast
    for rel in ("skills/bisheng-docx/scripts/render_docx.py", "skills/bisheng-pptx/scripts/render_deck.py"):
        source = (vendor_root() / rel).read_text()
        imports = [node for node in ast.walk(ast.parse(source)) if isinstance(node, (ast.Import, ast.ImportFrom))]
        assert all("fitz" not in ast.unparse(node) for node in imports)
        assert "pdftoppm" in source


@pytest.mark.asyncio
async def test_worker_does_not_finish_a_go_fenced_unknown_run():
    paths = []
    async def respond(request):
        paths.append(request.url.path)
        if request.url.path.endswith("chat/completions"):
            return httpx.Response(409, json={"error": {"code": "NEEDS_CONFIRMATION", "needs_confirmation": True}})
        return httpx.Response(200, json={"code": 0, "data": {}})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        result = await process_manifest("https://control.test", manifest(), client)
    assert result.status == "needs_confirmation"
    assert not any(path.endswith("/finish") for path in paths)


@pytest.mark.asyncio
async def test_heartbeat_loss_cancels_graph_without_finish(monkeypatch):
    import office_agent.worker as worker_module
    paths = []
    async def lost(control):
        await asyncio.sleep(0.02)
        raise RuntimeFailure("LEASE_LOST", "租约失效")
    async def respond(request):
        paths.append(request.url.path)
        if request.url.path.endswith("chat/completions"):
            await asyncio.sleep(1)
            return httpx.Response(200, json={"choices": [{"message": {"role": "assistant", "content": "late"}}]})
        return httpx.Response(200, json={"code": 0, "data": {}})
    monkeypatch.setattr(worker_module, "heartbeat_loop", lost)
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        result = await process_manifest("https://control.test", manifest(), client)
    assert result.error_code == "LEASE_LOST"
    assert not any(path.endswith("/finish") for path in paths)


@pytest.mark.asyncio
@pytest.mark.parametrize("status,uncertain,code", [("unknown", True, "EXECUTOR_RESULT_UNKNOWN"),
                                                 ("cancelled", True, "EXECUTOR_CANCELLED"),
                                                 ("timed_out", False, "EXECUTOR_TIMED_OUT"),
                                                 ("invalid_output", False, "INVALID_EXECUTOR_OUTPUT")])
async def test_executor_terminal_outcomes_stop_instead_of_model_retry(status, uncertain, code):
    from office_agent.executor import OfficeExecutorClient
    paths = []
    async def respond(request):
        paths.append(request.url.path)
        body = json.loads(request.content)
        if request.url.path.endswith("/prepare"):
            return httpx.Response(200, json={"execute": True})
        if request.url.path.endswith("/result"):
            assert body["output"]["status"] == status
            return httpx.Response(200, json={"state": "completed"})
        return httpx.Response(200, json={"version": 1, "run_id": str(item.run_id),
            "operation_id": body["operation_id"], "lease_epoch": item.lease_epoch, "status": status,
            "workspace_manifest": [], "outputs": [], "stdout": "", "stderr": ""})
    item = manifest()
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        control = ControlClient("https://control.test", item, client)
        executor = OfficeExecutorClient("https://executor.test", "key", client, control, VirtualWorkspace())
        with pytest.raises(RuntimeFailure) as failure:
            await executor.execute("print('x')", "same-tool-call")
    assert failure.value.code == code
    assert failure.value.uncertain == uncertain
    assert sum(path.startswith("/v1/runs/") for path in paths) == 1
    assert any(path.endswith("/result") for path in paths) == (not uncertain)


def test_long_assistant_message_matches_go_utf8_budget():
    item = manifest(messages=[{"role": "assistant", "content": "x" * 40000}])
    assert len(item.messages[0].content) == 40000
    with pytest.raises(ValueError):
        manifest(messages=[{"role": "assistant", "content": "中" * 40000}])


def test_fixed_skills_do_not_consume_task_file_or_byte_quota(monkeypatch):
    import base64
    import office_agent.workspace as workspace_module
    ws = VirtualWorkspace()
    assert len(ws.files) >= 38
    rows = []
    for number in range(128):
        value = b"a"
        rows.append({"path": f"scratch/{number}.txt", "size": 1, "sha256": hashlib.sha256(value).hexdigest(),
                     "content_base64": base64.b64encode(value).decode()})
    ws.replace_executor_snapshot(rows)
    assert len(ws.files) > 128
    assert ws.write("/scratch/too-many.txt", "x").error
    # Test exact byte ceiling using a small equivalent, without a 24 MiB allocation.
    monkeypatch.setattr(workspace_module, "MAX_WORKSPACE_BYTES", 128)
    assert ws._task_budget_ok(ws.files)
    assert not ws._task_budget_ok(ws.files | {"scratch/0.txt": b"aa"})


def test_executor_wire_cap_allows_one_full_snapshot_without_duplicate_bytes():
    from office_agent.executor import MAX_EXECUTOR_RESPONSE, MAX_EXECUTOR_SNAPSHOT
    base64_size = 4 * ((MAX_EXECUTOR_SNAPSHOT + 2) // 3)
    assert MAX_EXECUTOR_SNAPSHOT == 24 * 1024 * 1024
    assert MAX_EXECUTOR_RESPONSE == 40 * 1024 * 1024
    assert base64_size + 8 * 1024 * 1024 <= MAX_EXECUTOR_RESPONSE
    assert 2 * base64_size > MAX_EXECUTOR_RESPONSE  # former duplicated protocol was invalid
