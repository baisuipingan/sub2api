import base64
from copy import deepcopy
import hashlib
import json
import random
import struct
import zlib

import httpx
import pytest
from langchain_core.messages import AIMessage, HumanMessage, ToolMessage
from langgraph.types import Command, Interrupt, Send

from office_agent.checkpoint import decode, encode
from office_agent.checkpoint import GoCheckpointSaver
from office_agent.chat import ChatRuntime
from office_agent.contracts import RuntimeFailure
from office_agent.control import ControlClient
from office_agent.model import BridgeChatModel
from office_agent.resources import ResourceClient
from office_agent.runtime import OfficeRuntime
from office_agent.worker import claim, process_manifest
from office_agent.workspace import VirtualWorkspace
from office_agent.vision import image_message, render_reference
from test_runtime import manifest


class ByteStream(httpx.AsyncByteStream):
    def __init__(self, value):
        self.value = value

    async def __aiter__(self):
        for start in range(0, len(self.value), 7):
            yield self.value[start:start + 7]


@pytest.mark.asyncio
async def test_actual_sse_fragmented_tool_arguments_and_usage():
    frames = [
        {"choices": [{"delta": {"content": "中文"}}]},
        {"choices": [{"delta": {"tool_calls": [{"index": 0, "id": "call1", "function": {"name": "web_search", "arguments": '{"que'}}]}}]},
        {"choices": [{"delta": {"tool_calls": [{"index": 0, "function": {"arguments": 'ry":"资料"}'}}]}, "finish_reason": "tool_calls"}]},
        {"choices": [], "usage": {"prompt_tokens": 12, "completion_tokens": 6, "total_tokens": 18}},
    ]
    payload = b": keepalive\r\n\r\n" + b"".join(("data: " + json.dumps(frame, ensure_ascii=False) + "\r\n\r\n").encode() for frame in frames) + b"data: [DONE]\r\n\r\n"
    calls = []
    async def respond(request):
        calls.append(request)
        return httpx.Response(200, headers={"content-type": "text/event-stream"}, stream=ByteStream(payload))
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        answer = await BridgeChatModel(ControlClient("https://control.test", manifest(), client)).ainvoke([HumanMessage("test")])
    assert answer.content == "中文"
    assert answer.tool_calls == [{"name": "web_search", "args": {"query": "资料"}, "id": "call1", "type": "tool_call"}]
    assert answer.usage_metadata["total_tokens"] == 18
    assert answer.response_metadata["operation_id"].count("model-") == 1
    assert len(calls) == 1


@pytest.mark.asyncio
@pytest.mark.parametrize("stream_consumer", [False, True])
@pytest.mark.parametrize("arguments,finish,expected", [
    ('{"file_path":"/output/notes.txt","content":"unfinished', "length", "MODEL_OUTPUT_TRUNCATED"),
    ('{"file_path":"/output/notes.txt","content":"unfinished', "tool_calls", "INVALID_MODEL_TOOL_ARGUMENTS"),
    ('{"file_path":"/output/notes.txt","content":"complete"}', "length", "MODEL_OUTPUT_TRUNCATED"),
    ('{"file_path":"/output/notes.txt","content":"complete"}', "content_filter", "MODEL_TOOL_CALL_INCOMPLETE"),
    ('[]', "tool_calls", "INVALID_MODEL_TOOL_ARGUMENTS"),
    ('{"value":NaN}', "tool_calls", "INVALID_MODEL_TOOL_ARGUMENTS"),
])
async def test_streamed_tools_require_complete_strict_json_before_dispatch(arguments, finish, expected, stream_consumer):
    frames = [
        {"choices": [{"delta": {"tool_calls": [{"index": 0, "id": "write-1", "function": {"name": "write_file", "arguments": arguments}}]}}]},
        {"choices": [{"delta": {}, "finish_reason": finish}]},
    ]
    payload = "".join("data: " + json.dumps(frame) + "\n\n" for frame in frames) + "data: [DONE]\n\n"
    calls = []
    async def respond(request):
        calls.append(request)
        return httpx.Response(200, headers={"content-type": "text/event-stream"}, text=payload)
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        model = BridgeChatModel(ControlClient("https://control.test", manifest(), client))
        with pytest.raises(RuntimeFailure) as failure:
            if stream_consumer:
                async for _ in model.astream([HumanMessage("fixture")]):
                    pass
            else:
                await model.ainvoke([HumanMessage("fixture")])
    assert failure.value.code == expected
    assert not failure.value.uncertain and len(calls) == 1


@pytest.mark.asyncio
async def test_streamed_tool_names_are_checked_after_fragment_assembly():
    frames = [
        {"choices": [{"delta": {"tool_calls": [{"index": 0, "id": "search-1", "function": {"name": "web_", "arguments": '{"que'}}]}}]},
        {"choices": [{"delta": {"tool_calls": [{"index": 0, "function": {"name": "search", "arguments": 'ry":"资料"}'}}]}, "finish_reason": "tool_calls"}]},
    ]
    payload = "".join("data: " + json.dumps(frame) + "\n\n" for frame in frames) + "data: [DONE]\n\n"
    async def respond(request):
        return httpx.Response(200, headers={"content-type": "text/event-stream"}, text=payload)
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        answer = await BridgeChatModel(ControlClient("https://control.test", manifest(), client)).ainvoke([HumanMessage("fixture")])
    assert answer.tool_calls == [{"name": "web_search", "args": {"query": "资料"}, "id": "search-1", "type": "tool_call"}]


@pytest.mark.asyncio
@pytest.mark.parametrize("name,arguments,identity,expected", [
    ("write_file", '{"content":"unfinished', "tool-1", "INVALID_MODEL_TOOL_ARGUMENTS"),
    ("execute", '{}', "tool-1", "TOOL_NOT_ALLOWED"),
    ("web_search", '{}', None, "INVALID_MODEL_RESPONSE"),
])
async def test_completed_json_model_response_uses_same_strict_tool_validation(name, arguments, identity, expected):
    async def respond(request):
        return httpx.Response(200, json={"choices": [{"message": {"role": "assistant", "content": "", "tool_calls": [
            {"id": identity, "type": "function", "function": {"name": name, "arguments": arguments}}]}, "finish_reason": "tool_calls"}]})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        with pytest.raises(RuntimeFailure) as failure:
            await BridgeChatModel(ControlClient("https://control.test", manifest(), client)).ainvoke([HumanMessage("fixture")])
    assert failure.value.code == expected


@pytest.mark.asyncio
@pytest.mark.parametrize("status,confirmed,expected,uncertain", [
    (502, True, "MODEL_UPSTREAM_FAILED", False),
    (500, True, "MODEL_UPSTREAM_FAILED", False),
    (402, True, "MODEL_UPSTREAM_FAILED", False),
    (413, True, "MODEL_UPSTREAM_FAILED", False),
    (413, False, "MODEL_INPUT_TOO_LARGE", False),
    (502, False, "OPERATION_UNKNOWN", True),
    (500, False, "OPERATION_UNKNOWN", True),
])
async def test_completed_model_inbox_failure_is_distinct_from_unknown_transport(status, confirmed, expected, uncertain):
    calls = []
    async def respond(request):
        calls.append(request)
        headers = {"x-office-operation-state": "completed"} if confirmed else {}
        return httpx.Response(status, headers=headers, json={"error": {"code": "UPSTREAM_ERROR", "message": "upstream-private-body-and-credentials"}})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        with pytest.raises(RuntimeFailure) as failure:
            await BridgeChatModel(ControlClient("https://control.test", manifest(), client)).ainvoke([HumanMessage("fixture")])
    assert failure.value.code == expected and failure.value.uncertain == uncertain
    assert "upstream-private-body" not in failure.value.message
    if confirmed:
        assert str(status) in failure.value.message
    assert len(calls) == 1


@pytest.mark.asyncio
async def test_internal_billing_pending_without_completed_inbox_marker_is_preserved():
    async def respond(request):
        return httpx.Response(409, json={"error": {"code": "BILLING_PENDING"}})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        with pytest.raises(RuntimeFailure) as failure:
            await BridgeChatModel(ControlClient("https://control.test", manifest(), client)).ainvoke([HumanMessage("fixture")])
    assert failure.value.code == "BILLING_PENDING" and failure.value.uncertain


@pytest.mark.asyncio
async def test_chat_failed_search_is_reported_as_failed_tool_and_never_claimed_success():
    calls, events = [], []
    async def respond(request):
        if request.url.path.endswith("chat/completions"):
            calls.append(json.loads(request.content))
            message = {"role": "assistant", "content": "未取得联网资料"} if len(calls) == 2 else {
                "role": "assistant", "content": "", "tool_calls": [{"id": "search-failed", "type": "function", "function": {
                    "name": "web_search", "arguments": '{"query":"test"}'}}]}
            return httpx.Response(200, json={"choices": [{"message": message}]})
        if request.url.path.endswith("/search"):
            return httpx.Response(200, json={"code": 0, "data": {"error": "SEARCH_FAILED", "message": "检索失败"}})
        if request.url.path.endswith("/events"):
            events.append(json.loads(request.content))
            return httpx.Response(200, json={"code": 0, "data": {}})
        raise AssertionError(request.url.path)
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        result = await ChatRuntime(ControlClient("https://control.test", manifest(mode="chat", search_enabled=True), client)).execute()
    assert result.status == "completed" and len(calls) == 2
    completed = [e for e in events if e["type"] == "tool.completed"]
    assert len(completed) == 1 and completed[0]["payload"]["status"] == "failed"
    assert "SEARCH_FAILED" in next(m["content"] for m in calls[1]["messages"] if m["role"] == "tool")


def test_model_operation_identity_uses_logical_step_and_separate_compaction_sequence():
    control = ControlClient("https://control.test", manifest(), None)
    ordinary = BridgeChatModel(control)
    _, first = ordinary._request([HumanMessage("first")], None, {})
    _, second = ordinary._request([HumanMessage("second")], None, {})
    _, changed = BridgeChatModel(control)._request([HumanMessage("worker changed request bytes")], None, {})
    compaction = BridgeChatModel(control, purpose="context_compaction")
    _, compact_first = compaction._request([HumanMessage("summarize")], None, {})
    _, compact_second = compaction._request([HumanMessage("summarize again")], None, {})
    assert first == changed, "Go input-hash conflict must stop a changed replay under the same logical identity"
    assert len({first, second, compact_first, compact_second}) == 4


@pytest.mark.asyncio
async def test_incomplete_sse_stops_without_retry_or_republishing_delta():
    calls = []
    async def respond(request):
        calls.append(request.url.path)
        return httpx.Response(200, headers={"content-type": "text/event-stream"}, content='data: {"choices":[{"delta":{"content":"half"}}]}\n\n')
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        result = await ChatRuntime(ControlClient("https://control.test", manifest(mode="chat"), client)).execute()
    assert result.status == "needs_confirmation" and result.error_code == "MODEL_STREAM_INCOMPLETE"
    assert len(calls) == 1 and calls[0].endswith("chat/completions")


def test_safe_checkpoint_codec_roundtrip_and_no_class_construction():
    value = {"messages": [HumanMessage("问"), AIMessage("答", tool_calls=[{"id": "t", "name": "web_search", "args": {"query": "q"}}]),
                          ToolMessage("result", tool_call_id="t")],
             "send": Send("tools", {"key": 1}), "interrupt": Interrupt({"question": "q"}, id="i"),
             "command": Command(resume={"x": 1}), "tuple": (1, 2), "set": {3},
             "untrusted": {"__office_type__": "datetime", "value": "arbitrary user text"}}
    restored = decode(json.loads(json.dumps(encode(value))))
    assert restored == value
    for unsafe in [b"file bytes", object(), RuntimeError("secret")]:
        with pytest.raises(RuntimeFailure):
            encode(unsafe)
    with pytest.raises(ValueError):
        decode({"__office_type__": "arbitrary.module.Class", "value": {}})


class DurableState:
    def __init__(self):
        self.checkpoint, self.checkpoint_version = None, 0
        self.snapshots, self.head = {0: []}, 0
        self.events, self.waits, self.published = [], [], []

    def control(self, item, client):
        state = self
        class FakeControl(ControlClient):
            async def get(self, path, **kwargs):
                if path.endswith("checkpoint"):
                    return {"version": state.checkpoint_version, "data": deepcopy(state.checkpoint)}
                assert "/workspace" in path
                version = int(path.split("?version=")[1]) if "?version=" in path else state.head
                return {"version": version, "head_version": state.head, "snapshot_id": str(version), "files": deepcopy(state.snapshots[version])}

            async def post(self, path, body, **kwargs):
                if path.endswith("checkpoint"):
                    assert body["expected_version"] == state.checkpoint_version
                    state.checkpoint_version += 1
                    state.checkpoint = deepcopy(body["data"])
                    return {"version": state.checkpoint_version}
                if path.endswith("workspace"):
                    assert body["expected_version"] == state.head
                    state.head += 1
                    state.snapshots[state.head] = deepcopy(body["files"])
                    return {"version": state.head, "snapshot_id": str(state.head)}
                if path.endswith("wait"):
                    state.waits.append(body)
                    return {}
                if path.endswith("artifacts"):
                    state.published.append(body)
                    return {"resource_id": "r", "version_id": "v"}
                if path.endswith("events"):
                    state.events.append(body)
                    return {}
                raise AssertionError(path)
        return FakeControl("https://control.test", item, client)


@pytest.mark.asyncio
async def test_real_office_graph_failed_search_has_failed_public_tool_event():
    state, calls = DurableState(), []
    async def respond(request):
        calls.append(json.loads(request.content))
        message = {"role": "assistant", "content": "未取得联网资料"} if len(calls) == 2 else {
            "role": "assistant", "content": "", "tool_calls": [{"id": "search-failed", "type": "function", "function": {
                "name": "web_search", "arguments": '{"query":"test"}'}}]}
        return httpx.Response(200, json={"choices": [{"message": message}]})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        control = state.control(manifest(durable_resume=True, search_enabled=True), client)
        original_post = control.post
        async def failed_search(path, body, **kwargs):
            if path.endswith("search"):
                return {"error": "SEARCH_FAILED", "message": "检索失败"}
            return await original_post(path, body, **kwargs)
        control.post = failed_search
        ws = VirtualWorkspace(with_skills=False)
        result = await OfficeRuntime(control, workspace=ws, resources=ResourceClient(control, ws)).execute()
    assert result.status == "completed" and len(calls) == 2
    completed = [e for e in state.events if e["type"] == "tool.completed"]
    assert len(completed) == 1 and completed[0]["payload"]["status"] == "failed"


@pytest.mark.asyncio
async def test_real_graph_never_executes_sdk_repaired_truncated_file_tool():
    state, calls = DurableState(), []
    frames = [
        {"choices": [{"delta": {"tool_calls": [{"index": 0, "id": "write-truncated", "function": {
            "name": "write_file", "arguments": '{"file_path":"/output/notes.txt","content":"unfinished'}}]}}]},
        {"choices": [{"delta": {}, "finish_reason": "length"}]},
    ]
    payload = "".join("data: " + json.dumps(frame) + "\n\n" for frame in frames) + "data: [DONE]\n\n"
    async def respond(request):
        calls.append(request)
        return httpx.Response(200, headers={"content-type": "text/event-stream"}, text=payload)
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        control = state.control(manifest(durable_resume=True), client)
        ws = VirtualWorkspace(with_skills=False)
        result = await OfficeRuntime(control, workspace=ws, resources=ResourceClient(control, ws)).execute()
    assert result.status == "failed" and result.error_code == "MODEL_OUTPUT_TRUNCATED"
    assert len(calls) == 1 and not state.published
    assert "output/notes.txt" not in ws.files
    assert not any(event["type"] == "tool.started" for event in state.events)


@pytest.mark.asyncio
async def test_real_deepagents_clarification_persist_rebuild_resume_no_replayed_model():
    state, model_calls = DurableState(), []
    item = manifest(durable_resume=True)
    async def respond(request):
        body = json.loads(request.content)
        model_calls.append(body)
        message = {"role": "assistant", "content": "已按回答完成"} if len(model_calls) == 2 else {
            "role": "assistant", "content": "", "tool_calls": [{"id": "clarify-1", "type": "function", "function": {
                "name": "ask_user", "arguments": json.dumps({"title": "补充用途", "questions": [{"id": "use", "question": "用途？", "options": ["内部", "外部"]}]})}}]}
        return httpx.Response(200, json={"choices": [{"message": message, "finish_reason": "stop"}]})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        control = state.control(item, client)
        ws = VirtualWorkspace(with_skills=False)
        first_runtime = OfficeRuntime(control, workspace=ws, resources=ResourceClient(control, ws))
        first = await first_runtime.execute()
        assert first.status == "waiting_user", first
        assert len(state.waits) == 1 and len(model_calls) == 1
        persisted = json.dumps(state.checkpoint)
        assert "secret-test-lease" not in persisted and "content_base64" not in persisted
        reply = {"request_id": first.clarification["request_id"], "answers": [{"question_id": "use", "values": ["内部"]}]}
        resumed = item.model_copy(update={"lease_epoch": 2, "resume": True, "resume_value": reply, "checkpoint_version": state.checkpoint_version})
        control = state.control(resumed, client)
        ws = VirtualWorkspace(with_skills=False)
        result = await OfficeRuntime(control, workspace=ws, resources=ResourceClient(control, ws)).execute()
    assert result.status == "completed", result
    assert result.content == "已按回答完成"
    assert len(model_calls) == 2  # ask_user model step ran once, resumed final ran once
    assert any(message["role"] == "tool" and "内部" in message["content"] for message in model_calls[1]["messages"])


@pytest.mark.asyncio
async def test_real_deepagents_task_plan_uses_go_progress_status_contract():
    state, calls = DurableState(), []
    async def respond(request):
        calls.append(json.loads(request.content))
        if len(calls) == 1:
            message = {"role": "assistant", "content": "", "tool_calls": [{"id": "plan-1", "type": "function", "function": {
                "name": "write_todos", "arguments": json.dumps({"todos": [{"content": "检查报告", "status": "in_progress"}]})}}]}
        else:
            message = {"role": "assistant", "content": "已建立并执行计划"}
        return httpx.Response(200, json={"choices": [{"message": message}]})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        control = state.control(manifest(durable_resume=True), client)
        original_post = control.post
        async def strict_progress(path, body, **kwargs):
            if path.endswith("events") and body["type"] == "task.updated":
                allowed = {"pending", "in_progress", "completed", "failed", "cancelled"}
                if any(row["status"] not in allowed for row in body["payload"]["tasks"]):
                    raise RuntimeFailure("OFFICE_INVALID", "task status violates Go contract")
            return await original_post(path, body, **kwargs)
        control.post = strict_progress
        ws = VirtualWorkspace(with_skills=False)
        result = await OfficeRuntime(control, workspace=ws, resources=ResourceClient(control, ws)).execute()
    assert result.status == "completed", result
    tasks = [event["payload"]["tasks"] for event in state.events if event["type"] == "task.updated"]
    assert tasks and tasks[0][0]["status"] == "in_progress"
    assert len(calls) == 2


@pytest.mark.asyncio
@pytest.mark.parametrize("worker_prompt_changed", [False, True])
async def test_real_graph_crash_after_paid_model_before_checkpoint_replays_only_inbox(monkeypatch, worker_prompt_changed):
    class SimulatedProcessLoss(BaseException):
        pass
    state, paid, requests, inbox, inputs = DurableState(), [], [], {}, {}
    crashed = False
    async def respond(request):
        op = request.headers["x-office-operation-id"]
        requests.append(op)
        body = json.loads(request.content)
        if op in inputs and inputs[op] != body:
            return httpx.Response(409, json={"error": {"code": "OFFICE_CONFLICT"}})
        if op not in inbox:
            paid.append(op)
            inputs[op] = body
            inbox[op] = {"choices": [{"message": {"role": "assistant", "content": "这是已付费的原始回答"}}]}
        return httpx.Response(200, json=inbox[op])
    item = manifest(durable_resume=True)
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        control = state.control(item, client)
        original_post = control.post
        async def crash_before_checkpoint(path, body, **kwargs):
            nonlocal crashed
            if path.endswith("checkpoint") and paid:
                crashed = True
                raise SimulatedProcessLoss()  # Process loss, not a retriable HTTP error or graph cancellation.
            return await original_post(path, body, **kwargs)
        control.post = crash_before_checkpoint
        ws = VirtualWorkspace(with_skills=False)
        with pytest.raises(SimulatedProcessLoss):
            await OfficeRuntime(control, workspace=ws, resources=ResourceClient(control, ws)).execute()
        assert state.checkpoint and len(paid) == 1
        if worker_prompt_changed:
            import office_agent.runtime as runtime_module
            monkeypatch.setattr(runtime_module, "SYSTEM_PROMPT", runtime_module.SYSTEM_PROMPT + "\n新版固定提示约束。")
        resumed_control = state.control(item.model_copy(update={"lease_epoch": 2, "checkpoint_version": state.checkpoint_version}), client)
        restored = VirtualWorkspace(with_skills=False)
        result = await OfficeRuntime(resumed_control, workspace=restored, resources=ResourceClient(resumed_control, restored)).execute()
    if worker_prompt_changed:
        assert result.status == "failed" and result.error_code == "OFFICE_CONFLICT", result
    else:
        assert result.status == "completed", result
        assert result.content == "这是已付费的原始回答"
    assert len(requests) == 2 and requests[0] == requests[1], "the identical graph step must read the completed operation inbox"
    assert len(paid) == 1, "worker replacement must never create a second paid supplier operation"


@pytest.mark.asyncio
async def test_resource_exact_restore_uses_head_for_next_cas_and_publication():
    state = DurableState()
    payload = b"validated text"
    state.snapshots[1] = [{"path": "output/a.txt", "size": len(payload), "sha256": hashlib.sha256(payload).hexdigest(),
                           "mime": "text/plain", "content_base64": base64.b64encode(payload).decode()}]
    state.snapshots[2], state.head = [], 2  # orphan snapshot saved ahead of checkpoint
    async with httpx.AsyncClient() as client:
        control = state.control(manifest(), client)
        ws = VirtualWorkspace(with_skills=False)
        resources = ResourceClient(control, ws)
        await resources.restore(version=1)
        assert resources.version == 1 and resources.head_version == 2
        assert ws.files["output/a.txt"] == payload
        ws.write("/output/b.txt", "second")
        await resources.publish()
    assert state.head == 3
    assert len(state.published) == 2
    assert all(item["workspace_version"] == 3 for item in state.published)
    assert all("content_base64" not in item for item in state.published)


@pytest.mark.asyncio
async def test_chat_worker_has_no_office_tools_or_filesystem_dependency(monkeypatch):
    import office_agent.worker as worker
    monkeypatch.setattr(worker, "VirtualWorkspace", lambda **kwargs: pytest.fail("chat must not create office workspace"))
    async def respond(request):
        if request.url.path.endswith("chat/completions"):
            body = json.loads(request.content)
            assert "tools" not in body
            return httpx.Response(200, json={"choices": [{"message": {"content": "直接回答"}}]})
        return httpx.Response(200, json={})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        result = await process_manifest("https://control.test", manifest(mode="chat"), client, preset="chat")
    assert result.status == "completed"


@pytest.mark.asyncio
async def test_claim_separate_preset_and_control_query_allowlist():
    async def respond(request):
        if request.method == "POST":
            assert json.loads(request.content) == {"worker_id": "worker-test", "preset": "office", "executor_ready": None}
            return httpx.Response(204)
        assert request.url.query == b"version=0"
        return httpx.Response(200, json={"version": 0})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        assert await claim(client, "https://control.test", "bootstrap", "worker-test", "office") is None
        control = ControlClient("https://control.test", manifest(), client)
        assert (await control.get(control.run_path("workspace?version=0")))["version"] == 0
        for suffix in ["workspace?version=-1", "workspace?version=1&url=evil", "checkpoint?version=1", "workspace?version=1#fragment"]:
            with pytest.raises(RuntimeFailure):
                await control.get(control.run_path(suffix))


@pytest.mark.asyncio
async def test_office_completed_step_restores_snapshot_across_lease_without_execution():
    from office_agent.executor import OfficeExecutorClient
    state, stored, executed = DurableState(), {}, []
    item = manifest(durable_resume=True)
    payload = b"checked document"
    file = {"path": "output/a.txt", "size": len(payload), "sha256": hashlib.sha256(payload).hexdigest(),
            "mime": "text/plain", "content_base64": base64.b64encode(payload).decode()}
    async def respond(request):
        body = json.loads(request.content)
        op = request.headers["x-office-operation-id"]
        if request.url.path.endswith("prepare"):
            return httpx.Response(200, json={"execute": op not in stored, "result": stored.get(op)})
        if request.url.path.endswith("result"):
            # Snapshot must already be committed before a completed tool result.
            assert body["output"]["workspace_version"] in state.snapshots
            assert "content_base64" not in request.content.decode()
            stored[op] = body
            return httpx.Response(200, json={})
        executed.append(op)
        return httpx.Response(200, json={"version": 1, "run_id": str(item.run_id), "operation_id": op,
                "lease_epoch": body["lease_epoch"], "status": "completed", "exit_code": 0, "stdout": "checked", "stderr": "",
                "workspace_manifest": [file], "outputs": [{key: value for key, value in file.items() if key != "content_base64"}]})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        for epoch in [1, 2]:
            control = state.control(item.model_copy(update={"lease_epoch": epoch}), client)
            # Keep real tool prepare/result HTTP, while resource bridge is fake.
            original_post = control.post
            async def mixed_post(path, body, **kwargs):
                if "/tools/" in path:
                    return await ControlClient.post(control, path, body, **kwargs)
                return await original_post(path, body, **kwargs)
            control.post = mixed_post
            ws = VirtualWorkspace(with_skills=False)
            resources = ResourceClient(control, ws)
            await resources.restore(version=0)
            executor = OfficeExecutorClient("https://executor.test", "executor-only", client, control, ws, resources=resources)
            result = await executor.execute("print('check')", "same-graph-tool-step")
            assert result["status"] == "completed" and ws.files["output/a.txt"] == payload
    assert len(executed) == 1 and len(stored) == 1


@pytest.mark.asyncio
async def test_virtual_tool_budget_persists_and_same_step_does_not_double_count():
    state = DurableState()
    async with httpx.AsyncClient() as client:
        for _ in range(2):
            saver = GoCheckpointSaver(state.control(manifest(durable_resume=True), client))
            await saver.reserve_tool("same-step", 1)
            with pytest.raises(RuntimeFailure) as failure:
                await saver.reserve_tool("different-step", 1)
            assert failure.value.code == "TOOL_BUDGET_EXCEEDED"
    assert state.checkpoint["tool_operations"] == ["same-step"]


@pytest.mark.asyncio
async def test_modified_primary_output_creates_version_only_for_explicit_target():
    state = DurableState()
    item = manifest(output_parent={"resource_id": "original-resource", "version_id": "old-version", "path": "output/report.txt"})
    async with httpx.AsyncClient() as client:
        control = state.control(item, client)
        ws = VirtualWorkspace(with_skills=False)
        ws.write("/output/report.txt", "updated")
        ws.write("/output/notes.txt", "extra")
        await ResourceClient(control, ws).publish()
    primary = next(row for row in state.published if row["path"] == "output/report.txt")
    extra = next(row for row in state.published if row["path"] == "output/notes.txt")
    assert primary["resource_id"] == "original-resource" and primary["parent_version_id"] == "old-version"
    assert "resource_id" not in extra


@pytest.mark.asyncio
async def test_failed_resource_publication_never_returns_completed_or_fabricates_url():
    state = DurableState()
    async def respond(request):
        return httpx.Response(200, json={"choices": [{"message": {"content": "已处理"}}]})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        control = state.control(manifest(durable_resume=True), client)
        ws = VirtualWorkspace(with_skills=False)
        state.snapshots[0] = [{"path": "output/report.txt", "size": 1, "sha256": hashlib.sha256(b"a").hexdigest(),
                                "mime": "text/plain", "content_base64": "YQ=="}]
        resources = ResourceClient(control, ws)
        async def fail_publish():
            raise RuntimeFailure("TRANSPORT_UNKNOWN", "保存未确认", uncertain=True)
        resources.publish = fail_publish
        result = await OfficeRuntime(control, workspace=ws, resources=resources).execute()
    assert result.status == "needs_confirmation" and result.content == ""
    assert not state.published


PNG = base64.b64decode("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jP3sAAAAASUVORK5CYII=")


@pytest.mark.asyncio
async def test_real_graph_image_inspection_expands_only_at_model_boundary_not_checkpoint():
    state, model_calls = DurableState(), []
    def png_chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))

    # A valid, poorly compressible preview that still fits the image tool's limit.
    pixels = random.Random(42)
    scanlines = b"".join(b"\x00" + pixels.randbytes(300) for _ in range(300))
    preview = (PNG[:8] + png_chunk(b"IHDR", struct.pack(">IIBBBBB", 300, 300, 8, 0, 0, 0, 0))
               + png_chunk(b"IDAT", zlib.compress(scanlines)) + png_chunk(b"IEND", b""))
    encoded = base64.b64encode(preview).decode()
    result_file = b"Validated result."
    state.snapshots[0] = [
        {"path": path, "size": len(data), "sha256": hashlib.sha256(data).hexdigest(),
         "mime": mime, "content_base64": base64.b64encode(data).decode()}
        for path, data, mime in [("scratch/page.png", preview, "image/png"),
                                ("output/report.txt", result_file, "text/plain")]
    ]
    messages = [
        {"role": "user", "content": "请整理此前的资料"},
        {"role": "assistant", "content": "资料" * 12500},
        {"role": "user", "content": "请追加一份资料"},
        {"role": "assistant", "content": "补充" * 12500},
        {"role": "user", "content": "请检查文档版式并交付"},
    ]
    async def respond(request):
        body = json.loads(request.content)
        model_calls.append(body)
        if len(model_calls) == 1:
            assert len(request.content) < 256 * 1024
            assert "inspect_rendered_image" in {row["function"]["name"] for row in body["tools"]}
            message = {"role": "assistant", "content": "", "tool_calls": [{"id": "inspect1", "type": "function", "function": {
                "name": "inspect_rendered_image", "arguments": '{"path":"scratch/page.png"}'}}]}
        else:
            assert len(request.content) > 256 * 1024
            images = [message for message in body["messages"] if isinstance(message["content"], list)]
            assert len(images) == 1 and images[0]["role"] == "user"
            assert images[0]["content"][1]["image_url"]["url"] == "data:image/png;base64," + encoded
            message = {"role": "assistant", "content": "已检查渲染图"}
        return httpx.Response(200, json={"choices": [{"message": message}]})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        control = state.control(manifest(durable_resume=True, supports_vision=True, messages=messages), client)
        ws = VirtualWorkspace(with_skills=False)
        result = await OfficeRuntime(control, workspace=ws, resources=ResourceClient(control, ws), executor=object()).execute()
        assert result.status == "completed", result
        assert [file["path"] for file in state.published] == ["output/report.txt"]
        # A worker restarted after the final graph checkpoint must recover the
        # final answer, without silently losing messages or billing another call.
        restarted_control = state.control(control.manifest.model_copy(update={"lease_epoch": 2}), client)
        restarted_ws = VirtualWorkspace(with_skills=False)
        restarted = await OfficeRuntime(restarted_control, workspace=restarted_ws,
                                        resources=ResourceClient(restarted_control, restarted_ws), executor=object()).execute()
        assert restarted.status == "completed" and restarted.content == "已检查渲染图"
    checkpoint = json.dumps(state.checkpoint)
    assert "office_image_ref" in checkpoint
    assert "data:image" not in checkpoint and encoded not in checkpoint
    assert len(model_calls) == 2


def test_image_reference_rejects_cross_task_paths_oversize_dimensions_and_mutation():
    import struct
    ws = VirtualWorkspace(with_skills=False)
    ws.files["scratch/page.png"] = PNG
    ref = render_reference(ws, "scratch/page.png")
    for path in ["uploads/page.png", "/etc/passwd", "scratch/../page.png"]:
        with pytest.raises(RuntimeFailure):
            render_reference(ws, path)
    ws.files["scratch/page.png"] = PNG[:16] + struct.pack(">II", 65535, 65535) + PNG[24:]
    with pytest.raises(RuntimeFailure):
        render_reference(ws, "scratch/page.png")
    ws.files["scratch/page.png"] = b"changed"
    with pytest.raises(RuntimeFailure):
        image_message(ws, ref)


@pytest.mark.asyncio
async def test_plain_model_cannot_expand_a_forged_image_reference():
    ws = VirtualWorkspace(with_skills=False)
    ws.files["scratch/page.png"] = PNG
    async with httpx.AsyncClient() as client:
        model = BridgeChatModel(ControlClient("https://control.test", manifest(supports_vision=False), client), workspace=ws)
        with pytest.raises(RuntimeFailure) as failure:
            await model.ainvoke([HumanMessage("test"), ToolMessage("read", tool_call_id="t", additional_kwargs={"office_image_ref": render_reference(ws, "scratch/page.png")})])
    assert failure.value.code == "VISION_NOT_AVAILABLE"


@pytest.mark.asyncio
async def test_server_failure_after_durable_save_is_uncertain():
    async def respond(request):
        return httpx.Response(500, json={"error": {"code": "INTERNAL_SERVER_ERROR"}})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        control = ControlClient("https://control.test", manifest(), client)
        with pytest.raises(RuntimeFailure) as failure:
            await control.post(control.run_path("workspace"), {}, uncertain_on_transport=True)
    assert failure.value.uncertain


@pytest.mark.asyncio
@pytest.mark.parametrize("code,status,expected,uncertain", [
    ("BILLING_PENDING", 409, "needs_confirmation", True),
    ("OFFICE_CAPACITY", 503, "failed", False),
    ("RUN_LIMIT_EXCEEDED", 422, "failed", False),
])
async def test_model_admission_budget_errors_never_repeat_provider_request(code, status, expected, uncertain):
    calls = []
    async def respond(request):
        calls.append(request)
        return httpx.Response(status, json={"error": {"code": code}})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        control = ControlClient("https://control.test", manifest(), client)
        with pytest.raises(RuntimeFailure) as failure:
            async for _ in control.model_stream({"model": "public-model"}, "new-unpaid-step"):
                pass
    assert len(calls) == 1
    assert failure.value.code == code
    assert failure.value.uncertain is uncertain
    assert failure.value.result().status == expected
    if code == "BILLING_PENDING":
        assert "本步骤尚未调用" in failure.value.message


@pytest.mark.asyncio
@pytest.mark.parametrize("code", ["RUN_TIMEOUT", "MODEL_STREAM_INCOMPLETE", "ARTIFACT_PUBLICATION_UNKNOWN", "BILLING_PENDING"])
@pytest.mark.parametrize("suspend_status", [200, 401, 409])
async def test_worker_reports_unknown_outcome_once_without_reviving_fenced_run(monkeypatch, code, suspend_status):
    import office_agent.worker as worker_module
    from office_agent.contracts import RunResult
    calls = []
    async def stopped(self, progress):
        return RunResult(status="needs_confirmation", error_code=code, error_message="safe stop")
    async def respond(request):
        calls.append((request.url.path, json.loads(request.content)))
        assert request.url.path.endswith("/suspend")
        return httpx.Response(suspend_status, json={"error": {"code": "LEASE_LOST"}} if suspend_status != 200 else {"code": 0, "data": {}})
    monkeypatch.setattr(worker_module.ChatRuntime, "execute", stopped)
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        result = await process_manifest("https://control.test", manifest(mode="chat"), client)
    assert result.status == "needs_confirmation" and result.error_code == code
    assert len(calls) == 1 and calls[0][1] == {"code": code}


@pytest.mark.asyncio
async def test_long_context_is_billed_compacted_saved_then_answered_without_losing_question():
    ids = ["00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002", "00000000-0000-0000-0000-000000000003"]
    messages = [{"id": ids[0], "role": "user", "content": "历史需求" + "a" * 100000},
                {"id": ids[1], "role": "assistant", "content": "历史结果" + "b" * 100000},
                {"id": ids[2], "role": "user", "content": "现在请保留这句完整问题"}]
    item = manifest(mode="chat", messages=messages, current_question_id=ids[-1], context_window=1048576,
                    context_summary={"version": 5})
    order, bodies = [], []
    async def respond(request):
        body = json.loads(request.content)
        bodies.append(body)
        if request.url.path.endswith("context-summary"):
            order.append("save")
            assert body["expected_version"] == 5 and body["through_message_id"] == ids[1]
            assert body["source_hash"] == hashlib.sha256("\n".join(ids[:2]).encode()).hexdigest()
            return httpx.Response(200, json={"version": 6})
        assert request.url.path.endswith("chat/completions")
        compact = request.headers.get("x-office-model-purpose") == "context_compaction"
        order.append("compact" if compact else "answer")
        if compact:
            assert "历史需求" in body["messages"][1]["content"] and "历史结果" in body["messages"][1]["content"]
            content = "用户此前提出历史需求，已得到历史结果。"
        else:
            assert body["messages"][-1] == {"role": "user", "content": messages[-1]["content"]}
            assert any("已保存摘要" in row["content"] for row in body["messages"])
            content = "正式回答"
        return httpx.Response(200, json={"choices": [{"message": {"content": content}}],
                                        "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        result = await ChatRuntime(ControlClient("https://control.test", item, client)).execute()
    assert result.status == "completed", result
    assert order == ["compact", "save", "answer"]


@pytest.mark.asyncio
async def test_compaction_preflight_refuses_excess_calls_before_charging():
    calls = []
    async def respond(request):
        calls.append(request)
        pytest.fail("preflight must not spend a model call")
    item = manifest(mode="chat", context_window=20000, messages=[
        {"id": "1", "role": "user", "content": "a" * 100000},
        {"id": "2", "role": "assistant", "content": "b" * 100000},
        {"id": "3", "role": "user", "content": "current"}])
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        result = await ChatRuntime(ControlClient("https://control.test", item, client)).execute()
    assert result.error_code == "CONTEXT_COMPACTION_LIMIT" and not calls


@pytest.mark.asyncio
async def test_unknown_compaction_stops_before_saving_or_answer_call():
    paths = []
    async def respond(request):
        paths.append(request.url.path)
        assert request.headers["x-office-model-purpose"] == "context_compaction"
        raise httpx.ReadTimeout("no replay")
    item = manifest(mode="chat", messages=[{"id": "1", "role": "user", "content": "a" * 100000},
                    {"id": "2", "role": "assistant", "content": "b" * 100000}, {"id": "3", "role": "user", "content": "current"}])
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        result = await ChatRuntime(ControlClient("https://control.test", item, client)).execute()
    assert result.status == "needs_confirmation" and result.error_code == "TRANSPORT_UNKNOWN"
    assert len(paths) == 1


@pytest.mark.asyncio
@pytest.mark.parametrize("code,payload,ready", [(200, {"status": "ready", "execution_runtime": "runsc"}, True),
                                               (503, {"status": "cleanup_required", "execution_runtime": "runsc"}, False),
                                               (200, {"status": "ready", "execution_runtime": "runc"}, False)])
async def test_executor_health_reports_only_ready_gvisor(code, payload, ready):
    from office_agent.worker import executor_health
    async def respond(request):
        assert request.url.path == "/healthz" and "authorization" not in request.headers
        return httpx.Response(code, json=payload)
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        assert await executor_health(client, "https://executor.test") is ready


@pytest.mark.asyncio
async def test_valid_saved_summary_replaces_its_prefix_without_duplicate_history():
    ids = ["old-question", "old-answer", "current-question"]
    saved = {"version": 3, "content": "此前摘要", "through_message_id": ids[1],
             "source_hash": hashlib.sha256("\n".join(ids[:2]).encode()).hexdigest()}
    item = manifest(mode="chat", context_summary=saved, current_question_id=ids[2], messages=[
        {"id": ids[0], "role": "user", "content": "不要重复发送的旧原文"},
        {"id": ids[1], "role": "assistant", "content": "旧回答"},
        {"id": ids[2], "role": "user", "content": "本次问题"}])
    requests = []
    async def respond(request):
        requests.append(request)
        text = json.loads(request.content)["messages"]
        assert len(text) == 3 and "此前摘要" in text[1]["content"]
        assert "不要重复发送" not in request.content.decode()
        return httpx.Response(200, json={"choices": [{"message": {"content": "回答"}}]})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        result = await ChatRuntime(ControlClient("https://control.test", item, client)).execute()
    assert result.status == "completed" and len(requests) == 1


@pytest.mark.asyncio
async def test_new_compaction_hash_keeps_ids_from_previous_summary_boundary():
    ids = [str(index) for index in range(1, 6)]
    saved = {"version": 3, "content": "旧摘要", "through_message_id": ids[1],
             "source_hash": hashlib.sha256("\n".join(ids[:2]).encode()).hexdigest()}
    item = manifest(mode="chat", context_summary=saved, current_question_id=ids[4], messages=[
        {"id": ids[0], "role": "user", "content": "old"}, {"id": ids[1], "role": "assistant", "content": "old answer"},
        {"id": ids[2], "role": "user", "content": "a" * 100000}, {"id": ids[3], "role": "assistant", "content": "b" * 100000},
        {"id": ids[4], "role": "user", "content": "current"}])
    async def respond(request):
        body = json.loads(request.content)
        if request.url.path.endswith("context-summary"):
            assert body["source_hash"] == hashlib.sha256("\n".join(ids[:4]).encode()).hexdigest()
            assert body["through_message_id"] == ids[3] and body["expected_version"] == 3
            return httpx.Response(200, json={"version": 4})
        if request.headers.get("x-office-model-purpose"):
            assert "旧摘要" in body["messages"][1]["content"]
        return httpx.Response(200, json={"choices": [{"message": {"content": "整理或回答"}}]})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        result = await ChatRuntime(ControlClient("https://control.test", item, client)).execute()
    assert result.status == "completed", result


@pytest.mark.asyncio
async def test_artifact_preview_uses_exact_vendor_renderer_path():
    state = DurableState()
    async with httpx.AsyncClient() as client:
        control = state.control(manifest(), client)
        ws = VirtualWorkspace(with_skills=False)
        ws.files["output/report.docx"] = b"test document"
        ws.files["scratch/preview/report/report.pdf"] = b"test preview"
        ws.files["scratch/preview/other/other.pdf"] = b"unrelated preview"
        await ResourceClient(control, ws).publish()
    assert len(state.published) == 1
    assert state.published[0]["preview_path"] == "scratch/preview/report/report.pdf"


@pytest.mark.asyncio
async def test_same_stem_office_outputs_keep_separate_exact_previews():
    state = DurableState()
    async with httpx.AsyncClient() as client:
        control = state.control(manifest(), client)
        ws = VirtualWorkspace(with_skills=False)
        for extension in ("docx", "xlsx", "pptx"):
            ws.files[f"output/report.{extension}"] = b"test document"
            ws.files[f"scratch/preview/report.{extension}/report.pdf"] = extension.encode()
        ws.files["scratch/preview/report/report.pdf"] = b"ambiguous legacy preview"
        await ResourceClient(control, ws).publish()
    assert len(state.published) == 3
    for artifact in state.published:
        filename = artifact["path"].removeprefix("output/")
        assert artifact["preview_path"] == f"scratch/preview/{filename}/report.pdf"


@pytest.mark.asyncio
async def test_ambiguous_legacy_preview_is_not_attached_to_multiple_outputs():
    state = DurableState()
    async with httpx.AsyncClient() as client:
        control = state.control(manifest(), client)
        ws = VirtualWorkspace(with_skills=False)
        for path in ("output/report.xlsx", "output/report.pptx", "output/nested/report.docx"):
            ws.files[path] = b"test document"
        ws.files["scratch/preview/report/report.pdf"] = b"ambiguous legacy preview"
        ws.files["scratch/preview/nested/report.docx/report.pdf"] = b"exact nested preview"
        await ResourceClient(control, ws).publish()
    previews = {item["path"]: item.get("preview_path") for item in state.published}
    assert previews == {"output/report.xlsx": None, "output/report.pptx": None,
                        "output/nested/report.docx": "scratch/preview/nested/report.docx/report.pdf"}


@pytest.mark.asyncio
@pytest.mark.parametrize("uncertain,expected", [(False, "partially_completed"), (True, "needs_confirmation")])
async def test_real_graph_partial_archive_keeps_confirmed_results_without_retry(uncertain, expected):
    state, attempts = DurableState(), []
    state.snapshots[0] = [{"path": f"output/{name}.txt", "size": 1, "sha256": hashlib.sha256(b"a").hexdigest(),
                           "mime": "text/plain", "content_base64": "YQ=="} for name in ["a", "b"]]
    async def respond(request):
        return httpx.Response(200, json={"choices": [{"message": {"content": "已完成文档处理"}}]})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        control = state.control(manifest(durable_resume=True), client)
        original = control.post
        async def archive(path, body, **kwargs):
            if path.endswith("artifacts"):
                attempts.append(body["path"])
                if len(attempts) == 2:
                    raise RuntimeFailure("PUBLICATION_REJECTED", "第二项保存未完成", uncertain=uncertain)
            return await original(path, body, **kwargs)
        control.post = archive
        ws = VirtualWorkspace(with_skills=False)
        resources = ResourceClient(control, ws)
        result = await OfficeRuntime(control, workspace=ws, resources=resources).execute()
    assert result.status == expected and result.error_code == "PUBLICATION_REJECTED"
    assert attempts == ["output/a.txt", "output/b.txt"]
    assert len(resources.published) == 1 and len(state.published) == 1
    assert not any(event["type"] == "artifact.published" for event in state.events), "only Go archive transaction publishes artifact events"


@pytest.mark.asyncio
async def test_offloaded_tool_text_is_persistent_scratch_alias():
    state = DurableState()
    async with httpx.AsyncClient() as client:
        control = state.control(manifest(), client)
        ws = VirtualWorkspace(with_skills=False)
        assert ws.write("/large_tool_results/call.txt", "bounded tool output").error is None
        resources = ResourceClient(control, ws)
        await resources.persist()
        assert state.snapshots[1][0]["path"] == "scratch/large_tool_results/call.txt"
        restored = VirtualWorkspace(with_skills=False)
        await ResourceClient(control, restored).restore(version=1)
        assert restored.read("/large_tool_results/call.txt").file_data["content"] == "bounded tool output"
