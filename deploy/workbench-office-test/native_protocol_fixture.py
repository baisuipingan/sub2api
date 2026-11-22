"""Synthetic Gemini, Responses and Messages upstream for the isolated office test stack.

Send MILO_NATIVE_ACCEPTANCE for Word or MILO_NATIVE_FORMAT_<EXT> for the
additional formats. The upstream validates original signatures and tool receipts.
"""

from base64 import b64encode
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import argparse
import json
import re
from threading import Lock
import uuid

from native_office_formats import FORMAT_MARKER, FORMAT_STEPS, format_arguments, require


MARKER = "MILO_NATIVE_ACCEPTANCE"
CONTINUE = "MILO_NATIVE_CONTINUE"
RECOVER = "MILO_NATIVE_RECOVER"
FINAL = "文档已生成、修改并读回确认。"
FOLLOWUP = "已读到上一轮的文档操作。"
RECOVERED = "已收到中断前保存的文档版本。"
STEPS = ("create_docx", "read_docx", "edit_docx", "read_docx")
ACCEPTANCE_ID = re.compile(r"\bMILO_NATIVE_ACCEPTANCE ([0-9a-f]{32})\b")
PREFIXES = {}
PREFIX_LOCK = Lock()


def scenario_for(text):
    if CONTINUE in text:
        return "continue", None
    if MARKER in text:
        return "docx", None
    match = FORMAT_MARKER.search(text)
    if match:
        return match[1].lower(), match[2].split()
    return None


def scenario_steps(scenario):
    if scenario[0] == "continue":
        return ("read_docx",)
    return STEPS if scenario[0] == "docx" else FORMAT_STEPS[scenario[0]]


def scenario_arguments(scenario, receipts):
    require(len(receipts) <= len(scenario_steps(scenario)), "fixture got an unexpected tool receipt")
    if scenario[0] == "continue":
        if not receipts:
            return {"version_id": scenario[1]}
        texts = [section["text"] for section in receipts[0]["sections"]]
        require(any("新公司" in text for text in texts) and not any("旧公司" in text for text in texts),
                "follow-up did not reopen the edited document")
        return None
    if scenario[0] == "docx":
        return step_arguments(len(receipts), receipts)
    return format_arguments(*scenario, receipts)


def scenario_final(scenario):
    if scenario[0] == "continue":
        return FOLLOWUP
    if scenario[0] == "docx":
        return FINAL
    return f"{scenario[0].upper()} 文件处理已确认。"


def signature(protocol, index):
    return b64encode(f"workbench-{protocol}-signature-{index}".encode()).decode()


def call_id(prefix, scenario, index):
    return f"{prefix}_{scenario[0]}_{index}"


def continued_scenario(scenario, history_text, previous_receipts):
    if scenario is None or scenario[0] != "continue":
        return scenario
    require(FINAL in history_text, "follow-up lost the previous visible answer")
    require(len(previous_receipts) == len(STEPS), "follow-up lost an original native tool result")
    edited = previous_receipts[2]["artifact"]["version_id"]
    return "continue", str(uuid.UUID(edited))


def step_arguments(index, receipts):
    for name, result in zip(STEPS, receipts):
        require("error" not in result, f"{name} returned {result.get('error')}")
    if index == 0:
        return {"name": "native-acceptance.docx", "title": "旧公司交付单", "paragraphs": ["旧公司已完成交付。"]}
    first = receipts[0]["artifact"]["version_id"]
    require(uuid.UUID(first), "create did not return a version ID")
    if index == 1:
        return {"version_id": first}
    require(any("旧公司" in section["text"] for section in receipts[1]["sections"]), "original file read-back missing")
    if index == 2:
        return {"version_id": first, "edits": [{"old_text": "旧公司", "new_text": "新公司", "scope": "all"}]}
    edited = receipts[2]["artifact"]["version_id"]
    require(receipts[2]["verified"] and edited != first, "edit did not publish a verified new version")
    if index == 3:
        return {"version_id": edited}
    sections = [section["text"] for section in receipts[3]["sections"]]
    require(any("新公司" in text for text in sections) and not any("旧公司" in text for text in sections), "updated file read-back missing")
    return None


def gemini_receipts(contents):
    receipts, sent = [], []
    previous = []
    scenario = None
    for content in contents:
        for part in content.get("parts", []):
            if content.get("role") == "user" and (current := scenario_for(part.get("text") or "")):
                previous = receipts
                scenario, receipts, sent = current, [], []
            if scenario is None:
                continue
            if "functionCall" in part:
                sent.append(part)
            if "functionResponse" in part:
                response = part["functionResponse"]
                index = len(receipts)
                steps = scenario_steps(scenario)
                require(index < min(len(sent), len(steps)) and sent[index]["thoughtSignature"] == signature("gemini", index), "Gemini signature was lost")
                require(sent[index]["functionCall"]["name"] == response["name"] == steps[index], "Gemini response order or name changed")
                if index % 2 == 0:
                    require(response["id"] == call_id("gemini", scenario, index), "Gemini native call ID was lost")
                else:
                    require("id" not in response, "Gemini ID-less call acquired a native response ID")
                receipts.append(response["response"])
    return scenario, receipts, previous


def responses_receipts(items):
    receipts, sent = [], []
    previous = []
    scenario = None
    reasoning = None
    for item in items:
        if item.get("role") == "user" and (current := scenario_for(json.dumps(item.get("content", "")))):
            previous = receipts
            scenario, reasoning, receipts, sent = current, None, [], []
        if scenario is None:
            continue
        if item.get("type") == "reasoning":
            reasoning = item
        if item.get("type") == "function_call":
            sent.append((item, reasoning))
        if item.get("type") == "function_call_output":
            index = len(receipts)
            steps = scenario_steps(scenario)
            require(index < min(len(sent), len(steps)), "Responses call item was lost")
            call, encrypted = sent[index]
            require(encrypted and encrypted["encrypted_content"] == signature("responses", index), "Responses encrypted reasoning was lost")
            require(call["call_id"] == item["call_id"] == call_id("call_fixture", scenario, index)
                    and call["name"] == steps[index], "Responses call ID or name changed")
            receipts.append(json.loads(item["output"]))
    return scenario, receipts, previous


def gemini_stream(body):
    scenario, receipts, previous = gemini_receipts(body["contents"])
    parts = []
    user_text = [part["text"] for content in body["contents"] if content.get("role") == "user"
                 for part in content.get("parts", []) if part.get("text")]
    latest_user_text = user_text[-1] if user_text else ""
    if RECOVER in latest_user_text:
        filename = 'File delivered in this turn: "native-acceptance.docx"'
        version_id = str(uuid.UUID(latest_user_text.split()[-1]))
        earlier_text = "\n".join(part.get("text", "") for content in body["contents"][:-1] for part in content.get("parts", []))
        require(MARKER in earlier_text and filename + f" (version ID {version_id})" in earlier_text,
                "Gemini is missing the published version from the interrupted turn")
        yield {"candidates": [{"content": {"role": "model", "parts": [{"text": RECOVERED}]}, "finishReason": "STOP"}],
               "usageMetadata": {"promptTokenCount": 50, "candidatesTokenCount": 15, "totalTokenCount": 65}}
        return
    scenario = continued_scenario(scenario, "\n".join(part.get("text", "") for content in body["contents"]
                                  for part in content.get("parts", [])), previous)
    if scenario is not None:
        steps = scenario_steps(scenario)
        declarations = {tool["name"] for group in body["tools"] for tool in group.get("functionDeclarations", [])}
        require(set(steps) <= declarations, "Gemini did not receive the expected tool declarations")
        index = len(receipts)
        arguments = scenario_arguments(scenario, receipts)
        if arguments is not None:
            call = {"name": steps[index], "args": arguments}
            if index % 2 == 0:
                call["id"] = call_id("gemini", scenario, index)
            parts.append({"functionCall": call, "thoughtSignature": signature("gemini", index)})
    if not parts:
        parts.append({"text": scenario_final(scenario) if scenario is not None else "Gemini fixture ready."})
    usage = {"promptTokenCount": 50, "candidatesTokenCount": 15, "totalTokenCount": 65}
    if scenario and scenario[0] == "continue":
        usage["cachedContentTokenCount"] = 20
    yield {"candidates": [{"index": 0, "content": {"role": "model", "parts": parts}, "finishReason": "STOP"}],
           "usageMetadata": usage}


def responses_stream(body):
    items = body["input"] if isinstance(body.get("input"), list) else []
    scenario, receipts, previous = responses_receipts(items)
    scenario = continued_scenario(scenario, json.dumps(items, ensure_ascii=False), previous)
    response_id = "resp_fixture_" + uuid.uuid4().hex
    response = {"id": response_id, "object": "response", "model": body["model"], "status": "in_progress", "output": []}
    yield {"type": "response.created", "response": dict(response)}
    output = []
    if scenario is not None:
        steps = scenario_steps(scenario)
        tools = {tool["name"] for tool in body["tools"] if tool.get("type") == "function"}
        require(set(steps) <= tools, "Responses did not receive the expected tool declarations")
        require("reasoning.encrypted_content" in body["include"], "Responses omitted the encrypted reasoning request")
        index = len(receipts)
        arguments = scenario_arguments(scenario, receipts)
        if arguments is not None:
            output = [
                {"type": "reasoning", "id": call_id("rs_fixture", scenario, index), "summary": [], "encrypted_content": signature("responses", index)},
                {"type": "function_call", "id": call_id("fc_fixture", scenario, index), "status": "completed", "call_id": call_id("call_fixture", scenario, index),
                 "name": steps[index], "arguments": json.dumps(arguments, ensure_ascii=False)},
            ]
    if not output:
        text = scenario_final(scenario) if scenario is not None else "Responses fixture ready."
        message_id = "msg_fixture_" + uuid.uuid4().hex
        yield {"type": "response.output_text.delta", "item_id": message_id, "output_index": 0, "content_index": 0, "delta": text}
        output = [{"type": "message", "id": message_id, "role": "assistant", "phase": "final_answer", "status": "completed",
                   "content": [{"type": "output_text", "text": text, "annotations": []}]}]
    for index, item in enumerate(output):
        yield {"type": "response.output_item.done", "output_index": index, "item": item}
    usage = {"input_tokens": 50, "output_tokens": 15, "total_tokens": 65}
    if scenario and scenario[0] == "continue":
        usage["input_tokens_details"] = {"cached_tokens": 20}
    response.update(status="completed", output=output, usage=usage)
    yield {"type": "response.completed", "response": response}


def anthropic_receipts(messages):
    receipts, sent = [], []
    previous = []
    scenario = None
    for message in messages:
        signed = False
        redacted = False
        for part in message["content"]:
            if message["role"] == "user" and part["type"] == "text" and (current := scenario_for(part["text"])):
                previous = receipts
                scenario, receipts, sent = current, [], []
            if scenario is None:
                continue
            if part["type"] == "thinking":
                signed = part.get("signature") == signature("anthropic", len(receipts)) and part.get("thinking") == ""
            if part["type"] == "redacted_thinking":
                redacted = part.get("data") == signature("anthropic-redacted", len(receipts))
            if part["type"] == "tool_use":
                require(signed and redacted, "Claude changed an opaque thinking block")
                sent.append(part)
            if part["type"] == "tool_result":
                index = len(receipts)
                steps = scenario_steps(scenario)
                require(index < min(len(sent), len(steps)) and part["tool_use_id"] == sent[index]["id"] == call_id("toolu", scenario, index)
                        and sent[index]["name"] == steps[index], "Claude tool receipt ID or order changed")
                receipts.append(json.loads(part["content"]))
    return scenario, receipts, previous


def anthropic_stream(body):
    require(body.get("cache_control", {}).get("type") == "ephemeral", "Messages cache control did not reach upstream")
    scenario, receipts, previous = anthropic_receipts(body["messages"])
    scenario = continued_scenario(scenario, json.dumps(body["messages"], ensure_ascii=False), previous)
    blocks = []
    index = len(receipts)
    if scenario is not None:
        steps = scenario_steps(scenario)
        declarations = {tool["name"] for tool in body["tools"] if tool["input_schema"]["type"] == "object"}
        require(set(steps) <= declarations, "Claude did not receive Messages tool declarations")
        arguments = scenario_arguments(scenario, receipts)
        if arguments is not None:
            blocks.append(({"type": "thinking", "thinking": "", "signature": ""},
                           [{"type": "signature_delta", "signature": signature("anthropic", index)}]))
            blocks.append(({"type": "redacted_thinking", "data": signature("anthropic-redacted", index)}, []))
            encoded = json.dumps(arguments, ensure_ascii=False)
            blocks.append(({"type": "tool_use", "id": call_id("toolu", scenario, index), "name": steps[index], "input": {}},
                           [{"type": "input_json_delta", "partial_json": encoded[:16]},
                            {"type": "input_json_delta", "partial_json": encoded[16:]}]))
    finish = "tool_use" if blocks else "end_turn"
    if not blocks:
        text = scenario_final(scenario) if scenario is not None else "Claude fixture ready."
        blocks.append(({"type": "text", "text": ""}, [{"type": "text_delta", "text": text}]))
    usage = {"input_tokens": 30, "output_tokens": 0, "cache_creation_input_tokens": 20, "cache_read_input_tokens": 0}
    if scenario and scenario[0] == "continue":
        usage.update(cache_creation_input_tokens=10, cache_read_input_tokens=10)
    message = {"id": "msg_fixture_" + uuid.uuid4().hex, "type": "message", "role": "assistant", "model": body["model"],
               "content": [], "stop_reason": None, "stop_sequence": None, "usage": usage}
    yield {"type": "message_start", "message": message}
    for block_index, (block, deltas) in enumerate(blocks):
        yield {"type": "content_block_start", "index": block_index, "content_block": block}
        for delta in deltas:
            yield {"type": "content_block_delta", "index": block_index, "delta": delta}
        yield {"type": "content_block_stop", "index": block_index}
    yield {"type": "message_delta", "delta": {"stop_reason": finish, "stop_sequence": None}, "usage": {"output_tokens": 15}}
    yield {"type": "message_stop"}


def require_stable_prefix(provider, body):
    field = {"gemini": "contents", "responses": "input", "anthropic": "messages"}[provider]
    messages = body[field]
    previous_marker = ACCEPTANCE_ID.search(json.dumps(messages, ensure_ascii=False))
    if previous_marker is None:
        return
    if provider == "gemini":
        user_text = [part.get("text", "") for block in messages if block.get("role") == "user" for part in block["parts"] if "text" in part]
    elif provider == "anthropic":
        user_text = [part["text"] for block in messages if block.get("role") == "user" for part in block["content"] if part["type"] == "text"]
    else:
        user_text = [json.dumps(block["content"], ensure_ascii=False) for block in messages if block.get("role") == "user"]
    if not user_text or not (ACCEPTANCE_ID.search(user_text[-1]) or CONTINUE in user_text[-1]):
        return
    key = provider, previous_marker[1]
    stable_fields = ("model", "instructions", "system", "systemInstruction", "tools", "toolConfig", "tool_choice",
                     "cache_control", "generationConfig", "reasoning", "thinking", "output_config", "max_tokens", "max_output_tokens")
    stable = {name: body.get(name) for name in stable_fields}
    with PREFIX_LOCK:
        earlier = PREFIXES.get(key)
        if earlier:
            old_header, old_messages = earlier
            require(old_header == stable and messages[:len(old_messages)] == old_messages,
                    "sent prefix changed across model calls in one conversation")
            require(len(messages) > len(old_messages), "the next model call did not append messages")
        PREFIXES[key] = stable, messages


class FixtureHandler(BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        return

    def do_GET(self):
        self.send_json(200 if self.path == "/health" else 404, {"ok": self.path == "/health"})

    def do_POST(self):
        count = 0
        provider = {"/v1/messages": "anthropic", "/v1/messages?beta=true": "anthropic", "/v1/responses": "responses"}.get(self.path)
        if re.fullmatch(r"/v1beta/models/[^/:]+:streamGenerateContent\?alt=sse", self.path):
            provider = "gemini"
        if provider is None:
            print(f"unexpected fixture endpoint {self.path.split('?')[0]}", flush=True)
            return self.send_json(404, {"error": {"message": "fixture endpoint is unavailable"}})
        try:
            size = int(self.headers.get("Content-Length", 0))
            require(0 < size <= 1 << 20, "fixture body size exceeded")
            body = json.loads(self.rfile.read(size))
            if provider == "gemini":
                count = len(gemini_receipts(body["contents"])[1])
                events = list(gemini_stream(body))
            elif provider == "anthropic":
                count = len(anthropic_receipts(body["messages"])[1])
                events = list(anthropic_stream(body))
            else:
                items = body.get("input", [])
                count = len(responses_receipts(items)[1]) if isinstance(items, list) else 0
                events = list(responses_stream(body))
            require_stable_prefix(provider, body)
        except (KeyError, TypeError, ValueError) as error:
            print(f"{provider} error {error}", flush=True)
            return self.send_json(400, {"error": {"message": str(error), "type": "fixture_validation"}})
        if provider == "responses" and not body.get("stream"):
            return self.send_json(200, events[-1]["response"])
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        for event in events:
            if provider in {"responses", "anthropic"}:
                self.wfile.write(f"event: {event['type']}\n".encode())
            self.wfile.write(b"data: " + json.dumps(event, ensure_ascii=False).encode() + b"\n\n")
            self.wfile.flush()
        print(f"{provider} {count} receipt(s) verified", flush=True)

    def send_json(self, status, payload):
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--bind", default="127.0.0.1")
    parser.add_argument("--port", default=8877, type=int)
    options = parser.parse_args()
    ThreadingHTTPServer((options.bind, options.port), FixtureHandler).serve_forever()
