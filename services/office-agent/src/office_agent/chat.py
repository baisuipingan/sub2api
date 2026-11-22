"""Lightweight chat/search preset. No office graph, filesystem or executor."""
from __future__ import annotations

import asyncio
import json

from langchain_core.messages import AIMessage, HumanMessage, SystemMessage, ToolMessage

from .contracts import RunResult, RuntimeFailure
from .control import operation_id
from .model import BridgeChatModel
from .context import prepare_context
from .diagnostics import report_runtime_exception

SEARCH_TOOLS = [
    {"type": "function", "function": {"name": "web_search", "description": "搜索公开网页，回答时保留来源链接。",
     "parameters": {"type": "object", "properties": {"query": {"type": "string"}}, "required": ["query"], "additionalProperties": False}}},
    {"type": "function", "function": {"name": "web_read", "description": "读取公开网页的文字，禁止内网或登录页面。",
     "parameters": {"type": "object", "properties": {"url": {"type": "string"}}, "required": ["url"], "additionalProperties": False}}},
]


class ChatRuntime:
    def __init__(self, control):
        self.control = control

    async def execute(self, progress=None):
        manifest = self.control.manifest
        if manifest.resume or manifest.input_resources:
            return RunResult(status="failed", error_code="TASK_MODE_REQUIRED", error_message="处理文件或恢复办公任务请使用办公模式")
        messages = [SystemMessage(content="你是本站的对话助手。直接回答，不为普通问题额外规划。只有开启联网并实际取得资料才能声称联网；引用真实来源。需要生成或修改文件时，请提示用户开启「办公模式」。不编造文件或下载链接。")]
        model = BridgeChatModel(self.control)
        runnable = model.bind_tools(SEARCH_TOOLS) if manifest.search_enabled else model
        calls = 0
        stage = "context"
        try:
            async with asyncio.timeout(manifest.limits.max_run_seconds):
                context = await prepare_context(self.control, progress)
                messages += [HumanMessage(content=item["content"]) if item["role"] == "user" else AIMessage(content=item["content"]) for item in context]
                while True:
                    stage = "model"
                    answer = await runnable.ainvoke(messages)
                    messages.append(answer)
                    if not answer.tool_calls:
                        if not isinstance(answer.content, str) or not answer.content.strip():
                            raise RuntimeFailure("EMPTY_RESULT", "模型未返回可展示的回复")
                        if len(answer.content.encode()) > 96 * 1024:
                            raise RuntimeFailure("RESULT_TOO_LARGE", "回复超过保存上限")
                        return RunResult(status="completed", content=answer.content)
                    for call in answer.tool_calls:
                        calls += 1
                        if calls > manifest.limits.max_tool_calls:
                            raise RuntimeFailure("TOOL_BUDGET_EXCEEDED", "本次对话达到检索次数上限")
                        if not manifest.search_enabled or call["name"] not in {"web_search", "web_read"}:
                            raise RuntimeFailure("TOOL_NOT_ALLOWED", "当前对话未开放这项工具")
                        name = call["name"]
                        field, endpoint = ("query", "search") if name == "web_search" else ("url", "web-read")
                        args = call["args"]
                        if set(args) != {field} or not isinstance(args[field], str) or not args[field].strip():
                            raise RuntimeFailure("INVALID_SEARCH_ARGUMENT", "检索参数无效")
                        op = operation_id(str(manifest.run_id), "search", call["id"], args)
                        title = "正在搜索资料" if name == "web_search" else "正在阅读网页"
                        stage = "search"
                        await self.control.event("tool.started", {"operation_id": op, "tool_name": name, "title": title}, op + "-start")
                        result = await self.control.post(self.control.run_path(endpoint), {"operation_id": op, **args}, operation=op, uncertain_on_transport=True)
                        failed = isinstance(result, dict) and bool(result.get("error"))
                        await self.control.event("tool.completed", {"operation_id": op, "tool_name": name, "title": title, "status": "failed" if failed else "completed"}, op + "-done")
                        messages.append(ToolMessage(content=json.dumps(result, ensure_ascii=False), tool_call_id=call["id"], name=name,
                                                    status="error" if failed else "success"))
        except RuntimeFailure as exc:
            return exc.result()
        except TimeoutError:
            return RunResult(status="needs_confirmation", error_code="RUN_TIMEOUT", error_message="对话达到时间上限，未确认调用不会自动重发")
        except Exception as exc:
            report_runtime_exception("chat", manifest, exc, stage=stage)
            return RunResult(status="failed", error_code="CHAT_RUNTIME_ERROR", error_message="对话未能完成，请查看任务记录")
