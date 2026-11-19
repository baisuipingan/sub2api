"""A small LangChain chat-model adapter for the *existing* Go router bridge.

There is intentionally no vendor SDK, provider fallback, automatic retry, tracing
credential, or model key here. OpenAI-shaped protocol conversion happens in Go.
"""
from __future__ import annotations

import json
from typing import Any, Sequence

from langchain_core.language_models import BaseChatModel
from langchain_core.messages import AIMessage, AIMessageChunk, BaseMessage, ToolMessage, convert_to_openai_messages, message_chunk_to_message
from langchain_core.outputs import ChatGeneration, ChatGenerationChunk, ChatResult
from langchain_core.utils.function_calling import convert_to_openai_tool
from langgraph.config import get_config
from pydantic import PrivateAttr

from .contracts import RuntimeFailure
from .control import ControlClient, operation_id
from .vision import image_message

APPROVED_TOOLS = frozenset({"execute_office_python", "read_file", "write_file", "edit_file",
                            "ls", "glob", "grep", "write_todos", "ask_user", "web_search", "web_read", "inspect_rendered_image"})


class BridgeChatModel(BaseChatModel):
    model_name: str
    _control: ControlClient = PrivateAttr()
    _calls: int = PrivateAttr(default=0)
    _workspace: Any = PrivateAttr(default=None)
    _purpose: str | None = PrivateAttr(default=None)

    def __init__(self, control: ControlClient, *, workspace=None, purpose=None):
        super().__init__(model_name=control.manifest.model, cache=False)
        self._control = control
        self._workspace = workspace
        self._purpose = purpose

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

    def _request(self, messages, stop, kwargs):
        self._calls += 1
        manifest = self._control.manifest
        if self._calls > manifest.limits.max_model_calls:
            raise RuntimeFailure("MODEL_BUDGET_EXCEEDED", "本次任务已达到模型调用次数上限")
        if stop:
            raise RuntimeFailure("MODEL_PARAMETER_UNSUPPORTED", "模型桥不接受 stop 参数")
        formatted = []
        latest_image = next((index for index in range(len(messages) - 1, -1, -1)
                             if isinstance(messages[index], ToolMessage) and "office_image_ref" in messages[index].additional_kwargs), None)
        for index, message in enumerate(messages):
            formatted.extend(convert_to_openai_messages([message]))
        if latest_image is not None:
            if not manifest.supports_vision or self._workspace is None:
                raise RuntimeFailure("VISION_NOT_AVAILABLE", "当前任务模型未开放图片检查")
            # Complete every tool response in the assistant's tool-call group
            # before introducing a multimodal user message.
            formatted.append(image_message(self._workspace, messages[latest_image].additional_kwargs["office_image_ref"]))
        payload: dict[str, Any] = {"model": self.model_name,
                                  "messages": formatted,
                                  "max_tokens": min(manifest.limits.max_output_tokens, 4096) if self._purpose else manifest.limits.max_output_tokens, "stream": True,
                                  "stream_options": {"include_usage": True}}
        payload.update(manifest.parameters)
        if kwargs.get("tools"):
            payload.update(tools=kwargs["tools"], parallel_tool_calls=False)
        if kwargs.get("tool_choice") is not None:
            payload["tool_choice"] = kwargs["tool_choice"]
        if len(json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode()) > (1024 * 1024 if self._purpose else 256 * 1024):
            raise RuntimeFailure("MODEL_INPUT_TOO_LARGE", "任务上下文或渲染图超过模型桥输入上限，请减少资料或缩小预览")
        try:
            config = get_config()
        except RuntimeError:
            config = {}
        graph_step = config.get("metadata", {}).get("langgraph_step")
        step = f"main:{graph_step}:model" if graph_step is not None else f"chat:{self._calls}:model"
        if self._purpose:
            step = "context_compaction:" + str(self._calls)
        # Identity names the logical graph step, independent of request bytes.
        # Go stores a separate input hash: an upgraded worker changing a prompt
        # or schema must conflict with a completed step, never create a second
        # paid operation for the same execution position.
        return payload, operation_id(str(manifest.run_id), "model", step, None)

    @staticmethod
    def _chunk(result, op):
        if "_complete_response" in result:
            result = result["_complete_response"]
            complete = True
        else:
            complete = False
        choices = result.get("choices", [])
        choice = choices[0] if choices else {}
        item = choice.get("message" if complete else "delta", {})
        chunks = []
        for index, call in enumerate(item.get("tool_calls", [])):
            function = call.get("function", {})
            # Function names, just like arguments, may arrive in fragments.
            # Check the fully assembled name before the model returns a call.
            chunks.append({"name": function.get("name"), "args": function.get("arguments", ""), "id": call.get("id"),
                           "index": call.get("index", index), "type": "tool_call_chunk"})
        content = item.get("content") or ""
        if not isinstance(content, str):
            raise ValueError("model content must be text")
        # String metadata is concatenated by LangChain's chunk merger. Include
        # the operation identity only on the single finish chunk.
        metadata = {}
        if choice.get("finish_reason"):
            metadata["finish_reason"] = choice["finish_reason"]
            metadata["operation_id"] = op
        usage = result.get("usage")
        usage_metadata = None
        if usage:
            usage_metadata = {"input_tokens": usage.get("prompt_tokens", 0),
                              "output_tokens": usage.get("completion_tokens", 0),
                              "total_tokens": usage.get("total_tokens", 0)}
        return ChatGenerationChunk(message=AIMessageChunk(content=content, tool_call_chunks=chunks,
                                                          response_metadata=metadata, usage_metadata=usage_metadata))

    @staticmethod
    def _validate_complete(message):
        """Do not execute the SDK's permissively repaired partial tool JSON."""
        if not message.tool_call_chunks:
            return
        finish = message.response_metadata.get("finish_reason")
        if finish == "length":
            raise RuntimeFailure("MODEL_OUTPUT_TRUNCATED", "模型输出达到本次上限，工具请求不完整；本轮没有执行工具，请缩小任务或提高输出预算")
        if finish not in {None, "", "stop", "tool_calls"}:
            raise RuntimeFailure("MODEL_TOOL_CALL_INCOMPLETE", "模型未完整返回工具请求，本轮没有执行工具；请查看任务记录")
        for call in message.tool_call_chunks:
            if call.get("name") not in APPROVED_TOOLS:
                raise RuntimeFailure("TOOL_NOT_ALLOWED", "模型返回了未批准的工具调用")
            if not call.get("id"):
                raise RuntimeFailure("INVALID_MODEL_RESPONSE", "模型工具调用缺少稳定标识；本轮没有执行工具")
            try:
                def reject_constant(_):
                    raise ValueError("non-JSON numeric constant")
                args = json.loads(call.get("args", ""), parse_constant=reject_constant)
                if not isinstance(args, dict):
                    raise ValueError("tool arguments must be an object")
            except (TypeError, ValueError):
                raise RuntimeFailure("INVALID_MODEL_TOOL_ARGUMENTS", "模型工具参数不是完整 JSON；本轮没有执行工具") from None

    async def _astream(self, messages, stop=None, run_manager=None, **kwargs):
        payload, op = self._request(messages, stop, kwargs)
        accumulated = None
        try:
            async for result in self._control.model_stream(payload, op, purpose=self._purpose):
                chunk = self._chunk(result, op)
                accumulated = chunk if accumulated is None else accumulated + chunk
                yield chunk
            # This lives inside _astream so both ainvoke and SDK streaming
            # consumers validate before they can dispatch any assembled tool.
            if accumulated is not None:
                self._validate_complete(accumulated.message)
        except (KeyError, IndexError, TypeError, ValueError):
            raise RuntimeFailure("INVALID_MODEL_RESPONSE", "模型或工具响应无法识别；不会自动重试", uncertain=True) from None

    async def _agenerate(self, messages: list[BaseMessage], stop=None, run_manager=None, **kwargs) -> ChatResult:
        accumulated = None
        async for chunk in self._astream(messages, stop=stop, run_manager=run_manager, **kwargs):
            accumulated = chunk if accumulated is None else accumulated + chunk
        if accumulated is None:
            raise RuntimeFailure("EMPTY_MODEL_RESPONSE", "模型没有返回结果", uncertain=True)
        message = message_chunk_to_message(accumulated.message)
        if message.invalid_tool_calls:
            raise RuntimeFailure("INVALID_MODEL_RESPONSE", "模型工具参数无法识别；不会自动重试")
        return ChatResult(generations=[ChatGeneration(message=message)])
