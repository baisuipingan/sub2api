"""Explicit synthetic resource acceptance against the independent test site.

Does not call a model, copy production data, or print credentials/signed URLs.
"""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import secrets
import urllib.error
import urllib.parse
import urllib.request
import uuid


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--origin", default="https://llm.editor.vip")
    parser.add_argument("--credentials", type=Path, required=True)
    parser.add_argument("--evidence-dir", type=Path, required=True)
    args = parser.parse_args()
    origin = args.origin.rstrip("/")
    if urllib.parse.urlparse(origin).hostname not in {"llm.editor.vip", "localhost", "127.0.0.1"}:
        raise SystemExit("resource acceptance only permits the independent test origin")
    if args.credentials.stat().st_mode & 0o077:
        raise SystemExit("credentials file must be accessible only to its owner")
    admin = json.loads(args.credentials.read_text())["token"]
    prefix = "/api/v1/workbench/chat/resources"
    users, files, report = [], [], []

    def request(method, path, token=None, payload=None, headers=None, body=None):
        values = {"Accept": "application/json", **(headers or {})}
        if token:
            values["Authorization"] = "Bearer " + token
        if payload is not None:
            body = json.dumps(payload).encode()
            values["Content-Type"] = "application/json"
        req = urllib.request.Request(origin + path, data=body, headers=values, method=method)
        try:
            with urllib.request.urlopen(req, timeout=70) as response:
                raw = response.read(2 * 1024 * 1024)
                return response.status, json.loads(raw) if raw else {}
        except urllib.error.HTTPError as exc:
            # Never print request headers, token-bearing response URLs or bodies.
            return exc.code, {}

    def data(result, expected=200):
        status, value = result
        if status != expected:
            raise AssertionError("unexpected API HTTP status: " + str(status))
        return value.get("data", value)

    def upload(token, name, content, operation):
        boundary = "milo-acceptance-" + uuid.uuid4().hex
        body = ("--" + boundary + '\r\nContent-Disposition: form-data; name="file"; filename="' + name +
                '"\r\nContent-Type: application/octet-stream\r\n\r\n').encode() + content + ("\r\n--" + boundary + "--\r\n").encode()
        return request("POST", prefix, token, headers={"Content-Type": "multipart/form-data; boundary=" + boundary,
                                                        "Idempotency-Key": operation}, body=body)

    try:
        tokens = []
        for index in range(2):
            email = "office-resource-" + uuid.uuid4().hex + "@example.invalid"
            password = secrets.token_urlsafe(32)
            user = data(request("POST", "/api/v1/admin/users", admin,
                                {"email": email, "password": password, "role": "user", "balance": 0,
                                 "username": "文件隔离验收" + str(index), "concurrency": 1,
                                 "notes": "Independent synthetic office resource acceptance"}))
            users.append(user["id"])
            login = data(request("POST", "/api/v1/auth/login", payload={"email": email, "password": password}))
            tokens.append(login["access_token"])
        owner, stranger = tokens
        samples = [("acceptance.txt", "独立测试文件，不调用生图或文本模型。".encode())]
        for suffix in ("docx", "xlsx", "pptx"):
            samples.append(("acceptance." + suffix, (args.evidence_dir / ("output_acceptance." + suffix)).read_bytes()))
        samples.append(("preview.png", (args.evidence_dir / "scratch_preview_docx_page-01.png").read_bytes()))
        for name, content in samples:
            operation = "acceptance-" + uuid.uuid4().hex
            item = data(upload(owner, name, content, operation))
            files.append((owner, item["id"]))
            assert item["sha256"] == hashlib.sha256(content).hexdigest()
            assert item["bytes"] == len(content)
            repeated = data(upload(owner, name, content, operation))
            assert repeated["id"] == item["id"] and repeated["version_id"] == item["version_id"]
            access = data(request("GET", prefix + "/" + item["version_id"] + "/access", owner))
            with urllib.request.urlopen(access["url"], timeout=30) as response:
                downloaded = response.read(25 * 1024 * 1024)
            assert downloaded == content
            if name.endswith(".png"):
                assert access.get("preview_url")
                with urllib.request.urlopen(access["preview_url"], timeout=30) as response:
                    signature = response.read(12)
                    assert signature.startswith((b"\x89PNG\r\n\x1a\n", b"\xff\xd8\xff")) or (
                        signature[:4] == b"RIFF" and signature[8:12] == b"WEBP")
            assert request("GET", prefix + "/" + item["version_id"] + "/access", stranger)[0] == 404
            assert request("DELETE", prefix + "/" + item["id"], stranger)[0] == 404
            assert request("GET", prefix + "/" + item["version_id"] + "/access")[0] == 401
            report.append({"file": name, "upload": "ready", "stable_replay": True,
                           "private_download_hash": True, "cross_user": "not_found"})
        listed = data(request("GET", prefix, owner))["items"]
        assert {identifier for _, identifier in files}.issubset({item["id"] for item in listed})
        assert not data(request("GET", prefix, stranger))["items"]
        assert upload(owner, "invalid.png", b"not a PNG", "acceptance-invalid-" + uuid.uuid4().hex)[0] == 400
        assert upload(owner, "blocked.html", b"<script>alert(1)</script>", "acceptance-html-" + uuid.uuid4().hex)[0] == 400
        for token, identifier in files:
            data(request("DELETE", prefix + "/" + identifier, token))
        assert not data(request("GET", prefix, owner))["items"]
        print(json.dumps({"resource_api": report, "invalid_type": "rejected", "delete": "hidden", "models_called": 0}, ensure_ascii=False, indent=2))
    finally:
        for token, identifier in files:
            request("DELETE", prefix + "/" + identifier, token)
        for identifier in users:
            request("DELETE", "/api/v1/admin/users/" + str(identifier), admin)


if __name__ == "__main__":
    main()
