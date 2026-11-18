"""A small LangChain chat-model adapter for the *existing* Go router bridge.

There is intentionally no vendor SDK, provider fallback, automatic retry, tracing
credential, or model key here. OpenAI-shaped protocol conversion happens in Go.
"""
from __future__ import annotations

import json
from typing import Any, Sequence

from langchain_core.language_models import BaseChatModel
from langchain_core.messages import AIMessage, BaseMessage, convert_to_openai_messages
from langchain_core.outputs import ChatGeneration, ChatResult
from langchain_core.utils.function_calling import convert_to_openai_tool
from langgraph.config import get_config
from pydantic import PrivateAttr

from .contracts import RuntimeFailure
from .control import ControlClient, operation_id

APPROVED_TOOLS = frozenset({"execute_office_python", "read_file", "write_file", "edit_file",
                            "ls", "glob", "grep", "write_todos", "ask_user"})


class BridgeChatModel(BaseChatModel):
    model_name: str
    _control: ControlClient = PrivateAttr()
    _calls: int = PrivateAttr(default=0)

    def __init__(self, control: ControlClient):
        super().__init__(model_name=control.manifest.model, cache=False)
        self._control = control

    @property
    def _llm_type(self):
        return "office-bridge"

    def _get_ls_params(self, **kwargs):
        return {"ls_provider": "office-bridge", "ls_model_name": self.model_name, "ls_model_type": "chat"}

    @property
    def _identifying_params(self):
        return {"model_name": self.model_name}

    def bind_tools(self, tools: Sequence, *, tool_choice=None, **kwargs):
        formatted = [convert_to_openai_tool(tool) for tool in tools]
        if any(t.get("type") != "function" or t["function"]["name"] not in APPROVED_TOOLS for t in formatted):
            raise RuntimeFailure("TOOL_NOT_ALLOWED", "模型工具集合包含未批准的能力")
        options = {"tools": formatted}
        if tool_choice is not None:
            options["tool_choice"] = tool_choice
        return self.bind(**options)

    def _generate(self, messages, stop=None, run_manager=None, **kwargs):
        raise RuntimeFailure("SYNC_MODEL_DISABLED", "模型调用只支持受控异步桥")

    async def _agenerate(self, messages: list[BaseMessage], stop=None, run_manager=None, **kwargs) -> ChatResult:
        self._calls += 1
        manifest = self._control.manifest
        if self._calls > manifest.limits.max_model_calls:
            raise RuntimeFailure("MODEL_BUDGET_EXCEEDED", "本次任务已达到模型调用次数上限")
        if stop:
            raise RuntimeFailure("MODEL_PARAMETER_UNSUPPORTED", "试验模型桥尚不接受 stop 参数")
        payload: dict[str, Any] = {"model": self.model_name,
                                  "messages": convert_to_openai_messages(messages),
                                  "max_tokens": manifest.limits.max_output_tokens, "stream": False}
        if kwargs.get("tools"):
            payload.update(tools=kwargs["tools"], parallel_tool_calls=False)
        if kwargs.get("tool_choice") is not None:
            payload["tool_choice"] = kwargs["tool_choice"]
        try:
            config = get_config()
        except RuntimeError:
            config = {}
        meta = config.get("metadata", {})
        # Single-agent pilot: the main graph step is stable. Do not include
        # LangGraph's per-invocation task UUID/checkpoint namespace in the ID.
        step = f"main:{meta.get('langgraph_step', 0)}:model"
        op = operation_id(str(manifest.run_id), "model", step, payload)
        result = await self._control.post(manifest.bridge_path, payload, operation=op, uncertain_on_transport=True)
        try:
            choice = result["choices"][0]
            item = choice["message"]
            tool_calls = []
            for call in item.get("tool_calls", []):
                tool_name = call["function"]["name"]
                if tool_name not in APPROVED_TOOLS:
                    raise RuntimeFailure("TOOL_NOT_ALLOWED", "模型返回了未批准的工具调用")
                arguments = json.loads(call["function"]["arguments"])
                if not isinstance(arguments, dict):
                    raise ValueError("tool arguments must be an object")
                tool_calls.append({"id": call["id"], "name": tool_name, "args": arguments, "type": "tool_call"})
            usage = result.get("usage", {})
            ai = AIMessage(content=item.get("content") or "", tool_calls=tool_calls,
                           response_metadata={"finish_reason": choice.get("finish_reason"), "operation_id": op})
            if usage:
                ai.usage_metadata = {"input_tokens": usage.get("prompt_tokens", 0),
                                     "output_tokens": usage.get("completion_tokens", 0),
                                     "total_tokens": usage.get("total_tokens", 0)}
            return ChatResult(generations=[ChatGeneration(message=ai)])
        except (KeyError, IndexError, TypeError, ValueError):
            raise RuntimeFailure("INVALID_MODEL_RESPONSE", "上游返回的对话或工具参数无法识别；本次调用不会自动重试") from None
