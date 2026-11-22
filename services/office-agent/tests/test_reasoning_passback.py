import json

import httpx
import pytest

from office_agent.chat import ChatRuntime
from office_agent.checkpoint import decode, encode
from office_agent.control import ControlClient
from office_agent.model import BridgeChatModel
from test_runtime import manifest


@pytest.mark.asyncio
@pytest.mark.parametrize("stream,replay,effort", [(True, True, "high"), (False, True, ""), (True, False, "none")])
async def test_search_roundtrip_replays_only_enabled_private_tool_reasoning(stream, replay, effort):
    reason = "需要查证。"
    tool = {"id": "search-1", "type": "function", "function": {"name": "web_search", "arguments": '{"query":"资料"}'}}
    events = []
    model_requests = []

    async def respond(request):
        body = json.loads(request.content)
        if request.url.path.endswith("/events"):
            events.append(body)
            return httpx.Response(200, json={"code": 0, "data": {}})
        if request.url.path.endswith("/search"):
            return httpx.Response(200, json={"code": 0, "data": {"results": [{"title": "示例", "url": "https://example.test/"}]}})
        assert request.url.path.endswith("/chat/completions")
        model_requests.append(body)
        assert body.get("reasoning_effort", "") == effort
        if len(model_requests) == 2:
            assistant = next(message for message in body["messages"] if message.get("tool_calls"))
            if replay and assistant.get("reasoning_content") != reason:
                return httpx.Response(400, json={"error": {"message": "reasoning_content is required"}})
            assert assistant.get("reasoning_content") == (reason if replay else None)
            assert body["messages"][-1]["tool_call_id"] == "search-1"
            return httpx.Response(200, json={"choices": [{"message": {"content": "已找到资料"}, "finish_reason": "stop"}]})
        if not stream:
            item = {"role": "assistant", "reasoning_content": reason, "tool_calls": [tool]}
            return httpx.Response(200, json={"choices": [{"message": item, "finish_reason": "tool_calls"}]})
        chunks = [
            {"delta": {"reasoning_content": "需要"}},
            {"delta": {"reasoning_content": "查证。"}},
            {"delta": {"tool_calls": [{"index": 0, **tool}]}, "finish_reason": "tool_calls"},
        ]
        payload = "".join("data: " + json.dumps({"choices": [chunk]}) + "\n\n" for chunk in chunks)
        return httpx.Response(200, text=payload + "data: [DONE]\n\n", headers={"Content-Type": "text/event-stream"})

    item = manifest(mode="chat", search_enabled=True, replay_tool_reasoning=replay,
                    parameters={"reasoning_effort": effort} if effort else {})
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        result = await ChatRuntime(ControlClient("https://control.test", item, client)).execute()
    assert result.status == "completed" and result.content == "已找到资料"
    assert len(model_requests) == 2
    assert reason not in json.dumps(events, ensure_ascii=False)


def test_checkpoint_codec_preserves_private_reasoning_on_tool_message():
    from langchain_core.messages import message_chunk_to_message

    choice = {"message": {"reasoning_content": "工具调用上下文", "tool_calls": [{
        "id": "call-1", "function": {"name": "read_file", "arguments": '{"file_path":"/output/test"}'},
    }]}, "finish_reason": "tool_calls"}
    chunk = BridgeChatModel._chunk({"_complete_response": {"choices": [choice]}}, "op", capture_reasoning=True)
    message = message_chunk_to_message(chunk.message)
    restored = decode(encode(message))
    assert restored.tool_calls == message.tool_calls
    assert restored.additional_kwargs["reasoning_content"] == "工具调用上下文"
