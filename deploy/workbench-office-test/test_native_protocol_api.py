"""Run the native protocol acceptance against the isolated test stack."""

import argparse
from base64 import b64decode
from concurrent.futures import ThreadPoolExecutor
import io
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import time
import urllib.error
from urllib.parse import parse_qs, quote, urlsplit, urlunsplit
import urllib.request
import uuid
from zipfile import ZipFile

from native_office_formats import FORMAT_STEPS, TEXT_CONTENT


ROOT = Path("/opt/milo-workbench-test")
ORIGIN = "http://127.0.0.1:4000"
CHAT = "/api/v1/workbench/chat"
RESOURCES = CHAT + "/resources"
CONFIG = "/api/v1/admin/workbench/config"
ACCOUNTS = "/api/v1/admin/accounts"
STATE_FILE = ROOT / "secrets/native-protocol-acceptance.json"
FIXTURE_ORIGIN = "http://milo-workbench-native-fixture:8877"
MODELS = (
    ("gemini-3.6-flash-native-fixture", "gemini-3.6-flash", "gemini"),
    ("gpt-6-sol-native-fixture", "gpt-6-sol", "openai"),
    ("claude-sonnet-4-6-native-fixture", "claude-sonnet-4-6", "anthropic"),
)
PROTOCOLS = {"gemini": "gemini", "openai": "responses", "anthropic": "anthropic"}


class APIError(RuntimeError):
    def __init__(self, method, path, status, code):
        self.status, self.code = status, code
        super().__init__(f"{method} {path} returned HTTP {status} {code}")


def call(method, path, token=None, payload=None, body=None, headers=None):
    headers = {"Accept": "application/json", **(headers or {})}
    if token:
        headers["Authorization"] = "Bearer " + token
    if payload is not None:
        headers["Content-Type"] = "application/json"
        body = json.dumps(payload).encode()
    request = urllib.request.Request(ORIGIN + path, headers=headers, data=body, method=method)
    for attempt in range(3):
        try:
            with urllib.request.urlopen(request, timeout=90) as response:
                result = json.load(response)
            return result.get("data", result)
        except urllib.error.HTTPError as error:
            raw = error.read(4096)
            try:
                result = json.loads(raw)
                nested = result.get("error")
                code = (nested.get("code") if isinstance(nested, dict) else None) or result.get("reason") or result.get("code")
            except ValueError:
                code = ""
            retry_after = error.headers.get("Retry-After", "")
            if attempt < 2 and (error.code, code) == (429, "RATE_LIMITED") and retry_after.isdecimal():
                time.sleep(min(int(retry_after), 60) + 0.05)
                continue
            if attempt < 2 and error.code == 429 and path == "/api/v1/auth/login":
                time.sleep(60)
                continue
            raise APIError(method, path, error.code, code) from None


def login(email, password):
    return call("POST", "/api/v1/auth/login", payload={"email": email, "password": password})["access_token"]


def admin_token():
    file = ROOT / "secrets/compose.env"
    if ROOT.resolve() != ROOT or not file.is_file() or file.stat().st_mode & 0o077:
        raise RuntimeError("This script requires the isolated test stack and owner-only secrets")
    env = dict(line.split("=", 1) for line in file.read_text().splitlines() if "=" in line)
    return login("office-test@milo.invalid", env["ADMIN_PASSWORD"])


def store_state(data):
    temporary = STATE_FILE.with_suffix(".tmp")
    temporary.touch(mode=0o600, exist_ok=False)
    temporary.write_text(json.dumps(data))
    temporary.replace(STATE_FILE)


def config_patch(config, profiles):
    return {"image_studio_enabled": config["image_studio_enabled"],
            "image_studio_group_id": config["image_studio_group_id"],
            "model_profiles": profiles}


def setup(admin):
    if STATE_FILE.exists():
        raise RuntimeError("Previous fixture state exists. Run cleanup before re-creating accounts.")
    config = call("GET", CONFIG, admin)
    if config["agent_group_id"] != 3 or not all(config[flag] for flag in ("chat_enabled", "office_enabled", "files_enabled")):
        raise RuntimeError("Expected test Agent group 3 with chat and DOCX tools enabled")
    profiles = config.get("model_profiles") or {}
    if any(alias in profiles for alias, _, _ in MODELS):
        raise RuntimeError("Fixture profile name already in use")
    state = {"accounts": {}, "original_config": config}
    store_state(state)
    for alias, model, platform in MODELS:
        extras = {
            "openai": {"openai_responses_supported": True, "openai_responses_mode": "force_responses"},
            "anthropic": {"workbench_anthropic_prompt_cache": True},
        }
        account = call("POST", ACCOUNTS, admin, {
            "name": "Isolated native fixture " + platform,
            "platform": platform,
            "type": "apikey",
            "concurrency": 2,
            "priority": 10,
            "group_ids": [3],
            "credentials": {"api_key": "synthetic-test-token", "base_url": FIXTURE_ORIGIN, "model_mapping": {alias: model}},
            "extra": extras.get(platform, {}),
            "confirm_mixed_channel_risk": True,
        })
        state["accounts"][alias] = account["id"]
        store_state(state)
    config = call("GET", CONFIG, admin)
    profiles = dict(config.get("model_profiles") or {})
    profiles.update({alias: {"profile_id": model} for alias, model, _ in MODELS})
    call("PUT", CONFIG, admin, config_patch(config, profiles))
    print(f"Uploaded {len(MODELS)} temporary protocol fixture bindings", flush=True)


def cleanup(admin):
    if not STATE_FILE.exists():
        return
    state = json.loads(STATE_FILE.read_text())
    config = call("GET", CONFIG, admin)
    profiles = dict(config.get("model_profiles") or {})
    initial = dict(profiles)
    for alias, model, _ in MODELS:
        if profiles.get(alias, {}).get("profile_id") == model:
            del profiles[alias]
    if profiles != initial:
        call("PUT", CONFIG, admin, config_patch(config, profiles))
    for alias, account in list(state["accounts"].items()):
        call("DELETE", f"{ACCOUNTS}/{account}", admin)
        del state["accounts"][alias]
        store_state(state)
    STATE_FILE.unlink()
    print("Removed temporary fixture accounts and bindings", flush=True)


def fixture_database(sql):
    return subprocess.run(["docker", "exec", "-i", "milo-office-test-postgres-1", "psql", "-U", "office_test", "-d", "office_test", "-At", "-v", "ON_ERROR_STOP=1"],
                          input=sql, encoding="utf-8", capture_output=True, check=True).stdout.strip()


def cleanup_conversations(admin):
    users = fixture_database("""
        SELECT id, email FROM users WHERE deleted_at IS NULL AND role = 'user'
          AND email ~ '^office-native-[0-9a-f]{32}@example[.]invalid$' AND username = 'Native workbench acceptance'
    """)
    removed = 0
    for row in users.splitlines():
        user_id, email = row.split("|")
        if not re.fullmatch(r"office-native-[0-9a-f]{32}@example[.]invalid", email):
            continue
        password = secrets.token_urlsafe(32)
        call("PUT", f"/api/v1/admin/users/{int(user_id)}", admin, {"password": password})
        token = login(email, password)
        for conversation in call("GET", CHAT + "/conversations", token)["items"] or []:
            if conversation["title"].startswith("Native fixture "):
                call("DELETE", f"{CHAT}/conversations/{uuid.UUID(conversation['id'])}", token)
                removed += 1
        for artifact in call("GET", CHAT + "/resources", token)["items"] or []:
            if artifact["name"].startswith(("native-acceptance.", "native-batch-")):
                call("DELETE", f"{CHAT}/resources/{uuid.UUID(artifact['id'])}", token)
        call("DELETE", f"/api/v1/admin/users/{int(user_id)}", admin)
    print(f"Cleaned {removed} synthetic conversations and their test users", flush=True)


def verify_usage(run_id, account_id, expected_calls, expected_cache_reads=None):
    run_id = str(uuid.UUID(run_id))
    sql = f"""
        SELECT count(DISTINCT o.operation_id), count(l.id), count(DISTINCT l.request_id),
               count(*) FILTER (WHERE l.api_key_id = r.api_key_id AND l.account_id = {int(account_id)}),
               coalesce(sum(l.input_tokens), 0), coalesce(sum(l.output_tokens), 0),
               coalesce(sum(l.cache_read_tokens), 0)
        FROM office_agent_runs r
        JOIN office_agent_operations o ON r.id=o.run_id AND o.kind='model'
        LEFT JOIN usage_logs l ON l.request_id='client:'||o.request_id
        WHERE r.id='{run_id}'::uuid
    """
    for _ in range(15):
        totals = fixture_database(sql).split("|")
        numbers = [int(part) for part in totals]
        if numbers[:4] == [expected_calls] * 4 and min(numbers[4:6]) > 0:
            if expected_cache_reads is not None:
                assert numbers[6] == expected_cache_reads, f"Unexpected cache reads for {run_id}: {numbers[6]}"
            print(f"{run_id} {expected_calls} model calls and usage rows on the bound Agent key / fixture account", flush=True)
            return
        time.sleep(1)
    raise AssertionError(f"Unexpected usage totals for {run_id}: {numbers}")


def advance(token, path):
    try:
        return call("POST", path + "/advance", token)
    except APIError as error:
        if (error.status, error.code) != (409, "WORKBENCH_STEP_BUSY"):
            raise
        time.sleep(1)
        return call("GET", path, token)


def drive_turn(token, conversation, alias, prompt, versions=None):
    run = submit_turn(token, conversation, alias, prompt, versions)
    finish_turn(token, run)
    return run["id"]


def submit_turn(token, conversation, alias, prompt, versions=None):
    run_id = str(uuid.uuid4())
    return call("POST", f"{CHAT}/conversations/{conversation}/runs", token, {
        "run_id": run_id, "model": alias, "content": prompt, "web_search": False,
        "resource_version_ids": versions or [],
    })


def finish_turn(token, run):
    run_id = run["id"]
    path = f"{CHAT}/runs/{run_id}"
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        if run["status"] in {"completed", "failed", "cancelled", "partially_completed", "needs_confirmation"}:
            break
        run = advance(token, path)
    if run["status"] != "completed":
        raise AssertionError(f"Fixture run {run_id} ended {run['status']} {run.get('error_code')}")
    call("POST", path + "/advance", token)


def verify_interrupted_turn(token, account):
    alias = MODELS[0][0]
    conversation, run_id = str(uuid.uuid4()), str(uuid.uuid4())
    call("POST", CHAT + "/conversations", token, {"id": conversation, "title": "Native fixture interrupted"})
    resources = CHAT + "/resources"
    existing = {item["id"] for item in call("GET", resources, token)["items"] or []}
    call("POST", f"{CHAT}/conversations/{conversation}/runs", token, {
        "run_id": run_id, "model": alias, "content": "MILO_NATIVE_ACCEPTANCE", "web_search": False,
    })
    path = f"{CHAT}/runs/{run_id}"
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        advance(token, path)
        created = [item for item in call("GET", resources, token)["items"] if item["id"] not in existing]
        if created and any(tool["state"] == "completed" for tool in call("GET", path + "/tools", token)["items"]):
            break
    assert len(created) == 1 and created[0]["name"] == "native-acceptance.docx"
    cancel = call("DELETE", f"{CHAT}/runs/{run_id}", token)
    assert cancel["status"] == "cancelled"
    followup_id = drive_turn(token, conversation, alias, "MILO_NATIVE_RECOVER " + created[0]["version_id"])
    messages = call("GET", f"{CHAT}/conversations/{conversation}/messages", token)
    assert "已收到中断前保存的文档版本。" in json.dumps(messages, ensure_ascii=False)
    verify_usage(run_id, account, 1)
    verify_usage(followup_id, account, 1)


def create_user(admin):
    email = f"office-native-{uuid.uuid4().hex}@example.invalid"
    password = secrets.token_urlsafe(32)
    user = call("POST", "/api/v1/admin/users", admin, {
        "email": email, "password": password, "role": "user", "balance": 5,
        "username": "Native workbench acceptance", "concurrency": 2,
    })
    return user["id"], login(email, password)


def verify_details(token, stranger, run_id, steps, filename, partial=False):
    path = f"{CHAT}/runs/{run_id}/tools"
    tools = call("GET", path, token)["items"]
    assert [tool["name"] for tool in tools] == list(steps)
    assert {tool["state"] for tool in tools} == ({"partial"} if partial else {"completed"})
    details = []
    for tool in tools:
        detail = call("GET", path + "/" + quote(tool["id"], safe=""), token)
        assert detail["id"] == tool["id"] and detail["name"] == tool["name"] and detail["state"] == tool["state"]
        assert detail["duration_ms"] >= 0
        details.append(detail)
    for restricted in (path, path + "/" + quote(tools[0]["id"], safe="")):
        try:
            call("GET", restricted, stranger)
        except APIError as error:
            assert (error.status, error.code) == (404, "OFFICE_NOT_FOUND")
        else:
            raise AssertionError("A second user could see another user's tool records")
    products = [artifact for detail in details for artifact in detail.get("artifacts", [])]
    assert products and all(product["name"] == filename for product in products)
    restricted_resource = RESOURCES + f"/{uuid.UUID(products[-1]['version_id'])}/access"
    try:
        call("GET", restricted_resource, stranger)
    except APIError as error:
        assert error.status == 404
    else:
        raise AssertionError("A second user could obtain a private artifact link")
    return details, products[-1]


def download(token, version_id):
    access = call("GET", RESOURCES + f"/{uuid.UUID(version_id)}/access", token)
    return download_signed(access["url"])


def download_signed(signed_url):
    url = urlsplit(signed_url)
    headers = {}
    if url.netloc == "minio:9000":
        headers["Host"] = url.netloc
        url = url._replace(netloc="127.0.0.1:19000")
    assert (url.scheme, url.netloc) in {("http", "127.0.0.1:19000"), ("https", "llm.editor.vip")}
    queries = parse_qs(url.query)
    assert queries.get("X-Amz-Signature") and 0 < int(queries["X-Amz-Expires"][0]) <= 600
    request = urllib.request.Request(urlunsplit(url), headers=headers)
    with urllib.request.urlopen(request, timeout=30) as response:
        contents = response.read(4 << 20)
    assert 0 < len(contents) < 4 << 20
    return contents


def verify_print(admin, output):
    owner = create_user(admin)[1]
    stranger = create_user(admin)[1]
    conversation = new_conversation(owner, "print preview")
    run = drive_turn(owner, conversation, MODELS[0][0], "MILO_NATIVE_ACCEPTANCE")
    _, artifact = verify_details(owner, stranger, run, ("create_docx", "read_docx", "edit_docx", "read_docx"), "native-acceptance.docx")
    path = RESOURCES + f"/{uuid.UUID(artifact['version_id'])}"
    access = call("GET", path + "/access", owner)
    assert access["can_render_print_preview"] and not access.get("preview_url")
    try:
        call("POST", path + "/print-preview", stranger)
    except APIError as error:
        assert error.status == 404
    else:
        raise AssertionError("A second user could request another user's print preview")
    preview = call("POST", path + "/print-preview", owner)
    first_state = preview["status"]
    deadline = time.monotonic() + 60
    while preview["status"] == "pending" and time.monotonic() < deadline:
        time.sleep(min(max(preview.get("retry_after_ms", 1000), 100), 5000) / 1000)
        preview = call("POST", path + "/print-preview", owner)
    assert preview["status"] == "ready"
    signed_url = preview["preview_url"]
    rendered = download_signed(signed_url)
    assert rendered.startswith(b"%PDF-")
    preview_id = urlsplit(signed_url)._replace(query="", fragment="")
    for _ in range(2):
        replay = call("POST", path + "/print-preview", owner)
        assert replay["status"] == "ready"
        assert urlsplit(replay["preview_url"])._replace(query="", fragment="") == preview_id
    revisited = call("GET", path + "/access", owner)
    assert revisited["can_render_print_preview"] and not revisited.get("preview_url")
    if output is not None:
        with os.fdopen(os.open(output, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600), "wb") as handle:
            handle.write(rendered)
    print(f"On-demand print PDF verified with stable private object and owner access ({first_state} -> ready)", flush=True)


def new_conversation(token, title):
    conversation = str(uuid.uuid4())
    call("POST", CHAT + "/conversations", token, {"id": conversation, "title": "Native fixture " + title})
    return conversation


def verify_concurrency(admin):
    users = [create_user(admin)[1] for _ in range(3)]
    conversations = [new_conversation(user, "concurrency") for user in users]
    workbook_runs = [submit_turn(user, conversation, MODELS[index][0], "MILO_NATIVE_FORMAT_XLSX")
                     for index, (user, conversation) in enumerate(zip(users[:2], conversations[:2]))]

    def prepare_recalculation(user, run):
        path = f"{CHAT}/runs/{run['id']}"
        deadline = time.monotonic() + 45
        while time.monotonic() < deadline:
            run = advance(user, path)
            tools = call("GET", path + "/tools", user)["items"]
            if any(tool["name"] == "edit_xlsx" and tool["state"] == "completed" for tool in tools):
                # Ask the model for its next operation so the following advance starts recalculation.
                return advance(user, path)
            if run["status"] in {"failed", "cancelled", "needs_confirmation"}:
                break
        raise AssertionError(f"Unable to prepare Excel recalculation for {run['id']}")

    with ThreadPoolExecutor(max_workers=3) as executor:
        prep = [executor.submit(prepare_recalculation, user, run) for user, run in zip(users, workbook_runs)]
        ready = [future.result() for future in prep]
        started = time.monotonic()

        def finish_measured(user, run):
            finish_turn(user, run)
            return time.monotonic() - started

        offices = [executor.submit(finish_measured, user, run) for user, run in zip(users, ready)]
        observed = 0
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline and not all(future.done() for future in offices):
            observed = sum(any(tool["name"] == "recalc_xlsx" and tool["state"] == "running"
                               for tool in call("GET", f"{CHAT}/runs/{run['id']}/tools", user)["items"])
                           for user, run in zip(users, ready))
            if observed:
                break
            time.sleep(0.2)
        plain_start = time.monotonic()
        plain_run = submit_turn(users[2], conversations[2], MODELS[0][0], "普通对话并发验收。")
        plain = plain_run["id"]
        first_start = time.monotonic()
        plain_run = advance(users[2], f"{CHAT}/runs/{plain}")
        first_step = time.monotonic() - first_start
        finish_turn(users[2], plain_run)
        plain_elapsed = time.monotonic() - plain_start
        office_elapsed = [future.result() for future in offices]
    assert observed > 0, "plain chat did not overlap with a running Excel calculation"
    assert not call("GET", f"{CHAT}/runs/{plain}/tools", users[2])["items"]
    print(f"Plain chat {plain_elapsed:.2f}s with {observed} Excel recalculation(s) running at submission. "
          f"First model step: {first_step:.3f}s. "
          f"Office completions from simultaneous advance: {office_elapsed[0]:.2f}s and {office_elapsed[1]:.2f}s.", flush=True)


def upload_resource(token, name, contents, mime):
    boundary = "milo-acceptance-" + uuid.uuid4().hex
    header = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{name}"\r\n'
              f'Content-Type: {mime}\r\n\r\n').encode()
    multipart = header + contents + f"\r\n--{boundary}--\r\n".encode()
    return call("POST", RESOURCES, token, body=multipart, headers={
        "Content-Type": "multipart/form-data; boundary=" + boundary, "Idempotency-Key": str(uuid.uuid4()),
    })["version_id"]


def verify_batch(admin, token, stranger, conversation, alias, account, docx_version):
    source = io.BytesIO()
    with ZipFile(source, "w") as archive:
        archive.writestr("nested/delivery.docx", download(token, docx_version))
        archive.writestr("nested/broken.docx", b"malformed Word file")
        archive.writestr("keep.txt", b"unchanged")
    version_id = upload_resource(token, "native-batch-input.zip", source.getvalue(), "application/zip")
    run = drive_turn(token, conversation, alias, f"MILO_NATIVE_FORMAT_ZIP {version_id}", [version_id])
    details, artifact = verify_details(token, stranger, run, FORMAT_STEPS["zip"], "native-batch-edited.zip", partial=True)
    detail = details[0]
    assert [detail[key] for key in ("processed", "skipped", "failed")] == [1, 1, 1]
    assert detail["source_version_id"] == version_id
    admin_detail = call("GET", f"/api/v1/admin/workbench/chat/runs/{run}/tools/" + quote(detail["id"], safe=""), admin)
    assert admin_detail == detail
    with ZipFile(io.BytesIO(download(token, artifact["version_id"]))) as archive:
        assert set(archive.namelist()) == {"nested/delivery.docx", "nested/broken.docx", "keep.txt", "workbench-result.json"}
        assert archive.read("nested/broken.docx") == b"malformed Word file" and archive.read("keep.txt") == b"unchanged"
        with ZipFile(io.BytesIO(archive.read("nested/delivery.docx"))) as edited:
            xml = edited.read("word/document.xml").decode()
            assert "新公司" in xml and "旧公司" not in xml
        manifest = json.loads(archive.read("workbench-result.json"))
        files = {entry["name"]: entry for entry in manifest["files"]}
        assert {name: file["status"] for name, file in files.items()} == {
            "nested/delivery.docx": "completed", "nested/broken.docx": "failed", "keep.txt": "skipped",
        }
        returned = {entry["name"]: entry for entry in detail["files"]}
        assert returned.keys() == files.keys()
        assert returned["nested/delivery.docx"]["replacements"] == sum(files["nested/delivery.docx"]["replacements"])
    verify_usage(run, account, 2)
    print("ZIP partial delivery, per-entry manifest, and admin tool details verified", flush=True)


def verify_pptx_picture(token, stranger, alias, account):
    sample_script = '''
from base64 import b64encode
from io import BytesIO
import json
from PIL import Image
from pptx import Presentation
from pptx.util import Inches

def png(color):
    output = BytesIO()
    Image.new("RGB", (4, 4), color).save(output, format="PNG")
    output.seek(0)
    return output

deck = Presentation()
slide = deck.slides.add_slide(deck.slide_layouts[1])
slide.shapes.title.text = "Picture replacement fixture"
slide.shapes.add_picture(png("red"), Inches(1), Inches(2), width=Inches(1))
original = BytesIO()
deck.save(original)
print(json.dumps([b64encode(original.getvalue()).decode(), b64encode(png("blue").getvalue()).decode()]))
'''
    raw = subprocess.run(["docker", "exec", "-i", "milo-office-test-office-tools-1", "python", "-"],
                         input=sample_script, text=True, capture_output=True, check=True).stdout
    deck, picture = [b64decode(contents) for contents in json.loads(raw)]
    deck_version = upload_resource(token, "native-acceptance.pptx", deck,
                                   "application/vnd.openxmlformats-officedocument.presentationml.presentation")
    picture_version = upload_resource(token, "native-acceptance.png", picture, "image/png")
    conversation = new_conversation(token, "PPTX picture")
    run = drive_turn(token, conversation, alias, f"MILO_NATIVE_FORMAT_PPTX_IMAGE {deck_version} {picture_version}",
                     [deck_version, picture_version])
    _, artifact = verify_details(token, stranger, run, FORMAT_STEPS["pptx_image"], "native-acceptance.pptx")
    with ZipFile(io.BytesIO(download(token, artifact["version_id"]))) as edited:
        images = [edited.read(name) for name in edited.namelist() if name.startswith("ppt/media/") and name.endswith(".png")]
        assert images == [picture]
    verify_usage(run, account, 4)
    print("PPTX picture replaced from an authorized PNG, with exact embedded bytes", flush=True)


def verify(admin, office_formats=True):
    state = json.loads(STATE_FILE.read_text())
    user, token = create_user(admin)
    _, stranger = create_user(admin)
    catalog = call("GET", CHAT + "/models", token)
    models = {item["id"]: item for item in catalog["models"]}
    for alias, _, platform in MODELS:
        model = models[alias]
        assert model["protocol"] == PROTOCOLS[platform] and model["supports_tools"]
        conversation = str(uuid.uuid4())
        call("POST", CHAT + "/conversations", token, {"id": conversation, "title": "Native fixture " + platform})
        run_id = drive_turn(token, conversation, alias, "MILO_NATIVE_ACCEPTANCE " + uuid.uuid4().hex)
        details, _ = verify_details(token, stranger, run_id, ("create_docx", "read_docx", "edit_docx", "read_docx"), "native-acceptance.docx")
        original = details[0]["artifacts"][0]["version_id"]
        followup_id = drive_turn(token, conversation, alias, "MILO_NATIVE_CONTINUE")
        page = call("GET", f"{CHAT}/conversations/{conversation}/messages", token)
        final = json.dumps(page, ensure_ascii=False)
        assert "文档已生成、修改并读回确认。" in final
        assert "已读到上一轮的文档操作。" in final
        artifacts = call("GET", CHAT + "/resources", token)
        assert "native-acceptance.docx" in json.dumps(artifacts, ensure_ascii=False)
        verify_usage(run_id, state["accounts"][alias], 5)
        verify_usage(followup_id, state["accounts"][alias], 2, expected_cache_reads=20 if platform == "anthropic" else 40)
        print(f"{platform}: retained the upstream request prefix and reopened the prior file version", flush=True)
        if not office_formats or platform == "anthropic":
            continue
        if platform == "gemini":
            verify_batch(admin, token, stranger, conversation, alias, state["accounts"][alias], original)
        for suffix in (("xlsx", *TEXT_CONTENT) if platform == "gemini" else ("pptx",)):
            format_conversation = str(uuid.uuid4())
            call("POST", CHAT + "/conversations", token, {"id": format_conversation, "title": f"Native fixture {suffix}"})
            format_run = drive_turn(token, format_conversation, alias, f"MILO_NATIVE_FORMAT_{suffix.upper()}")
            format_details, artifact = verify_details(token, stranger, format_run, FORMAT_STEPS[suffix], f"native-acceptance.{suffix}")
            if suffix == "xlsx":
                assert format_details[2]["cells_changed"] == 2
                assert format_details[3]["title"] == "重算表格公式"
                assert format_details[3]["calculated"] == 1 and format_details[3]["formula_errors"] == 0
            if suffix in TEXT_CONTENT:
                assert download(token, artifact["version_id"]).decode() == TEXT_CONTENT[suffix].replace("旧公司", "新公司")
            verify_usage(format_run, state["accounts"][alias], len(FORMAT_STEPS[suffix]) + 1)
            print(f"{suffix.upper()} create, read, edit, and read-back verified", flush=True)
        if platform == "openai":
            verify_pptx_picture(token, stranger, alias, state["accounts"][alias])
    verify_interrupted_turn(token, state["accounts"][MODELS[0][0]])
    print(f"Verified {len(MODELS)} native protocols for synthetic user {user}", flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["setup", "verify", "verify-print", "verify-concurrency", "cleanup", "cleanup-conversations"])
    parser.add_argument("--pdf-output", type=Path)
    parser.add_argument("--prefix-only", action="store_true", help="check protocol, billing and DOCX prefixes across turns")
    args = parser.parse_args()
    actions = {"setup": setup, "verify": lambda admin: verify(admin, not args.prefix_only), "verify-print": lambda admin: verify_print(admin, args.pdf_output),
               "verify-concurrency": verify_concurrency, "cleanup": cleanup, "cleanup-conversations": cleanup_conversations}
    actions[args.action](admin_token())
