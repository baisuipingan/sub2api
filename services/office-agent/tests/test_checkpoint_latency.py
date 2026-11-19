"""Exercise the real graph with network-like persistence, not immediate mocks."""
import asyncio
import json

import httpx
import pytest

from office_agent.resources import ResourceClient
from office_agent.runtime import OfficeRuntime
from office_agent.workspace import VirtualWorkspace
from test_durable_chat import DurableState
from test_runtime import manifest


@pytest.mark.asyncio
async def test_long_graph_waits_for_durable_steps_under_storage_latency():
    state, calls = DurableState(), []

    async def respond(request):
        calls.append(json.loads(request.content))
        step = len(calls)
        if step <= 8:
            name = "write_file" if step % 2 else "write_todos"
            args = ({"file_path": f"/output/part-{step}.txt", "content": f"verified part {step}"}
                    if step % 2 else {"todos": [{"content": f"Review part {step}", "status": "completed"}]})
            message = {"role": "assistant", "content": "", "tool_calls": [{
                "id": f"step-{step}", "type": "function", "function": {
                    "name": name, "arguments": json.dumps(args)}}]}
        else:
            message = {"role": "assistant", "content": "All four files are ready."}
        return httpx.Response(200, json={"choices": [{"message": message, "finish_reason": "stop"}]})

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        item = manifest(durable_resume=True, limits={"max_model_calls": 16, "max_tool_calls": 24,
                        "max_output_tokens": 8192, "max_run_seconds": 30})
        control = state.control(item, client)
        original = control.post

        async def delayed_persistence(path, body, **kwargs):
            if path.endswith(("checkpoint", "workspace")):
                await asyncio.sleep(0.005)
            return await original(path, body, **kwargs)

        control.post = delayed_persistence
        workspace = VirtualWorkspace(with_skills=False)
        result = await OfficeRuntime(control, workspace=workspace,
                                     resources=ResourceClient(control, workspace)).execute()

    assert result.status == "completed", result
    assert len(calls) == 9 and len(state.published) == 4
    assert 1 <= len(state.checkpoint["records"]) <= 4
    assert state.checkpoint_version > 40


@pytest.mark.asyncio
async def test_vendored_skills_are_discovered_by_the_pinned_framework():
    from deepagents.middleware.skills import _alist_skills_with_errors

    skills, error = await _alist_skills_with_errors(VirtualWorkspace(), "/skills/")
    assert error is None
    assert {skill["name"] for skill in skills} == {"bisheng-docx", "bisheng-xlsx", "bisheng-pptx"}
