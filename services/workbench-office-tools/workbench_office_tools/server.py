"""Authenticated HTTP interface for the remote Office worker."""

from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
import fcntl
import hashlib
import hmac
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import logging
import os
from pathlib import Path
import re
import shutil
import signal
import sqlite3
import subprocess
import sys
import tempfile
import threading
import time
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, ProxyHandler, Request, build_opener
from uuid import uuid4

from .files import MAX_FILE, SHA256, OfficeError, file_description, file_name, object_list
from .operations import ACTIONS

JOB_PATH = re.compile(r"^/v1/jobs/([A-Za-z0-9_-]{1,128})(/output|/upload)?$")
VERSION = re.compile(r"^[a-f0-9-]{32,64}$")
REQUEST_LIMIT = 256 * 1024
MAX_CONCURRENCY = 8
MAX_CONCURRENCY_REVISION = (1 << 63) - 1
JOB_TIMEOUT = 120
OUTPUT_TTL = 3600
CACHE_TTL = 1200
RECEIPT_TTL = 7 * 86400
CACHE_LIMIT = 96 << 20
OUTPUT_LIMIT = 384 << 20
IMAGE_FILE_LIMIT = 8 << 20
IMAGE_COUNT_LIMIT = 16
IMAGE_SUFFIXES = {".png", ".jpg", ".jpeg"}
LOG = logging.getLogger(__name__)


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, *_args, **_kwargs):
        return None


DOWNLOAD = build_opener(ProxyHandler({}), NoRedirect())


def origin(url: str) -> str:
    target = urlsplit(url)
    if (
        target.scheme not in {"https", "http"} or not target.hostname
        or target.username or target.password or target.fragment
        or any(ch.isspace() or ord(ch) < 32 for ch in url)
    ):
        raise OfficeError("文件地址无效。", "INPUT_UNAVAILABLE")
    port = target.port
    default_port = 443 if target.scheme == "https" else 80
    host = f"[{target.hostname.lower()}]" if ":" in target.hostname else target.hostname.lower()
    suffix = f":{port}" if port is not None and port != default_port else ""
    return f"{target.scheme}://{host}{suffix}"


@dataclass(frozen=True)
class InputFile:
    version_id: str
    name: str
    sha256: str
    bytes: int
    url: str

    @classmethod
    def from_json(cls, raw: dict, suffix: str, origins: set[str]):
        if not isinstance(raw, dict):
            raise OfficeError("请指定输入文件。", "INVALID_ARGUMENT")
        version, digest, size, url = (raw.get(key) for key in ("version_id", "sha256", "bytes", "url"))
        if not isinstance(version, str) or not VERSION.fullmatch(version):
            raise OfficeError("文件版本无效。", "INVALID_ARGUMENT")
        if not isinstance(digest, str) or not SHA256.fullmatch(digest):
            raise OfficeError("文件校验值无效。", "INVALID_ARGUMENT")
        if type(size) is not int or not 0 < size <= MAX_FILE:
            raise OfficeError("输入文件超过存储上限。", "FILE_TOO_LARGE")
        if not isinstance(url, str) or len(url) > 8192 or origin(url) not in origins:
            raise OfficeError("文件地址不在配置的存储范围内。", "INPUT_UNAVAILABLE")
        return cls(version, file_name(raw.get("name"), suffix), digest, size, url)

    @property
    def identity(self):
        return self.version_id, self.name, self.sha256, self.bytes


def image_attachments(action: str, args: dict, raw: object, origins: set[str]) -> list[InputFile]:
    descriptors = object_list([] if raw is None else raw, IMAGE_COUNT_LIMIT, allow_empty=True)
    if action != "edit_pptx":
        if descriptors:
            raise OfficeError("此操作不支持图片附件。", "INVALID_ARGUMENT")
        return []
    images = {}
    for descriptor in descriptors:
        name = descriptor.get("name")
        suffix = Path(name).suffix.lower() if isinstance(name, str) else ""
        if suffix not in IMAGE_SUFFIXES:
            raise OfficeError("替换图片需要使用 PNG 或 JPEG 文件。", "INVALID_ARGUMENT")
        image = InputFile.from_json(descriptor, suffix, origins)
        if image.bytes > IMAGE_FILE_LIMIT:
            raise OfficeError("单张替换图片超过处理上限。", "FILE_TOO_LARGE")
        if image.version_id in images:
            raise OfficeError("替换图片版本重复。", "INVALID_ARGUMENT")
        images[image.version_id] = image
    if sum(image.bytes for image in images.values()) > MAX_FILE:
        raise OfficeError("替换图片总大小超过处理上限。", "FILE_TOO_LARGE")
    used = set()
    for edit in object_list(args.get("replace_images", []), 32, allow_empty=True):
        if "image_version_id" not in edit:
            continue
        version_id = edit["image_version_id"]
        if not isinstance(version_id, str) or version_id not in images or "image_base64" in edit:
            raise OfficeError("替换图片版本无效或未提供对应的附件。", "INVALID_ARGUMENT")
        used.add(version_id)
    if used != images.keys():
        raise OfficeError("替换图片附件与请求不一致。", "INVALID_ARGUMENT")
    return [images[version_id] for version_id in sorted(images)]


class JobService:
    def __init__(self, state_dir: Path, origins: set[str], concurrency: int, recalc_enabled: bool = False):
        self.root = state_dir.resolve()
        self.root.mkdir(mode=0o700, exist_ok=True, parents=True)
        self.cache = self.root / "inputs"
        self.outputs = self.root / "outputs"
        self.temporary = self.root / "tmp"
        for directory in (self.cache, self.outputs, self.temporary):
            directory.mkdir(mode=0o700, exist_ok=True)
        self.lockfile = (self.root / "service.lock").open("a")
        fcntl.flock(self.lockfile, fcntl.LOCK_EX | fcntl.LOCK_NB)
        identity_file = self.root / "node-id"
        if not identity_file.exists():
            temporary_identity = self.root / ".node-id.tmp"
            temporary_identity.write_text(str(uuid4()), encoding="ascii")
            temporary_identity.replace(identity_file)
        self.node_id = identity_file.read_text(encoding="ascii")
        self.storage_origins = origins
        self.limit = concurrency
        self.concurrency_revision = 0
        self.recalc_enabled = recalc_enabled
        self.lock = threading.RLock()
        self.execution_slots = threading.Condition(self.lock)
        self.download_locks = [threading.Lock() for _ in range(16)]
        self.active = {}
        self.executing = set()
        self.pool = ThreadPoolExecutor(max_workers=MAX_CONCURRENCY)
        self.connection = sqlite3.connect(self.root / "jobs.sqlite", check_same_thread=False)
        self.connection.execute("PRAGMA journal_mode=WAL")
        self.connection.execute(
            """CREATE TABLE IF NOT EXISTS jobs (
              id TEXT PRIMARY KEY, fingerprint TEXT NOT NULL, receipt TEXT NOT NULL,
              state TEXT NOT NULL, updated REAL NOT NULL
            )"""
        )
        interrupted = json.dumps(self.failure("WORKER_INTERRUPTED", "办公节点重启，本次文件处理已中断。"))
        self.connection.execute(
            "UPDATE jobs SET receipt=?, state='failed', updated=? WHERE state='running'",
            (interrupted, time.time()),
        )
        self.connection.commit()
        for child in self.temporary.iterdir():
            if child.is_dir():
                shutil.rmtree(child)
        self.next_gc = 0.0

    @staticmethod
    def failure(code: str, message: str) -> dict:
        return {"status": "failed", "error": code, "message": message}

    def cleanup(self) -> None:
        now = time.time()
        if now < self.next_gc:
            return
        self.next_gc = now + 60
        for child in self.outputs.iterdir():
            if not child.is_file():
                continue
            row = self.connection.execute("SELECT updated, state FROM jobs WHERE id=?", (child.name,)).fetchone()
            if row is None or row[1] not in ("completed", "partial") or row[0] < now - OUTPUT_TTL:
                if child.name not in self.active:
                    child.unlink(missing_ok=True)
        self.connection.execute("DELETE FROM jobs WHERE updated<? AND state<>'running'", (now - RECEIPT_TTL,))
        self.connection.commit()
        try:
            files = sorted((file.stat().st_mtime, file) for file in self.cache.iterdir() if file.is_file())
            total = sum(file.stat().st_size for _, file in files)
            for date, file in files:
                if date < now - CACHE_TTL or total > CACHE_LIMIT:
                    total -= file.stat().st_size
                    file.unlink(missing_ok=True)
        except FileNotFoundError:
            # An in-flight download links into or refreshes the cache concurrently.
            pass

    def status(self) -> dict:
        with self.lock:
            self.cleanup()
            return {
                "node_id": self.node_id,
                "version": os.environ.get("OFFICE_TOOLS_VERSION", "1"),
                "actions": sorted(action for action in ACTIONS if action != "recalc_xlsx" or self.recalc_enabled),
                "concurrency": self.limit,
                "concurrency_revision": self.concurrency_revision,
                "running": len(self.executing),
                "queued": len(self.active) - len(self.executing),
                "awaiting_upload": sum(
                    1 for file in self.outputs.iterdir()
                    if file.is_file() and not self.output_delivered(file.name)
                ),
                "storage_origins": sorted(self.storage_origins),
            }

    def configure_concurrency(self, payload: object) -> dict:
        if not isinstance(payload, dict):
            raise OfficeError("办公节点并发配置无效。", "INVALID_ARGUMENT")
        limit, revision = payload.get("concurrency"), payload.get("revision")
        if (
            type(limit) is not int or not 1 <= limit <= MAX_CONCURRENCY
            or type(revision) is not int or not 1 <= revision <= MAX_CONCURRENCY_REVISION
        ):
            raise OfficeError("办公节点并发配置无效。", "INVALID_ARGUMENT")
        with self.execution_slots:
            if revision > self.concurrency_revision:
                self.limit = limit
                self.concurrency_revision = revision
                self.execution_slots.notify_all()
            return self.status()

    def start(self, job_id: str, raw: object, pooled: bool = False) -> tuple[int, dict]:
        if not isinstance(raw, dict):
            raise OfficeError("办公任务参数无效。", "INVALID_ARGUMENT")
        action, args = raw.get("action"), raw.get("args", {})
        if not isinstance(action, str) or action not in ACTIONS or not isinstance(args, dict):
            raise OfficeError("办公操作或参数无效。", "INVALID_ARGUMENT")
        if action == "recalc_xlsx" and not self.recalc_enabled:
            return 503, self.failure("CALC_UNAVAILABLE", "办公节点尚未启用 Excel 公式重算。")
        suffix, _ = ACTIONS[action]
        source = None
        if suffix:
            source = InputFile.from_json(raw.get("input"), suffix, self.storage_origins)
        elif raw.get("input") is not None:
            raise OfficeError("此操作应从空白文件开始。", "INVALID_ARGUMENT")
        images = image_attachments(action, args, raw.get("images"), self.storage_origins)
        fields = [action, args, source.identity if source else None]
        if images:
            fields.append([image.identity for image in images])
        identity = json.dumps(fields, sort_keys=True, separators=(",", ":"), allow_nan=False)
        fingerprint = hashlib.sha256(identity.encode()).hexdigest()
        with self.lock:
            self.cleanup()
            row = self.connection.execute("SELECT fingerprint,receipt FROM jobs WHERE id=?", (job_id,)).fetchone()
            if row:
                if row[0] == "cancelled-before-dispatch":
                    return 200, json.loads(row[1])
                if row[0] != fingerprint:
                    return 409, self.failure("JOB_ID_CONFLICT", "操作编号已经用于不同的文件请求。")
                receipt = self.load_receipt(job_id, row[1])
                if receipt.get("error") == "WORKER_BUSY":
                    return 503, receipt
                return (202 if receipt["status"] == "running" else 200), receipt
            limit = self.limit if pooled else self.limit * 3
            output_reserved = sum(file.stat().st_size for file in self.outputs.iterdir() if file.is_file())
            if len(self.active) >= limit or output_reserved + (len(self.active) + 1) * MAX_FILE > OUTPUT_LIMIT:
                receipt = self.failure("WORKER_BUSY", "办公节点没有可用的处理位置。")
                if pooled:
                    # The same attempt must remain rejected if a delayed request arrives after reassignment.
                    self.connection.execute(
                        "INSERT INTO jobs(id,fingerprint,receipt,state,updated) VALUES(?,?,?,?,?)",
                        (job_id, fingerprint, json.dumps(receipt), "failed", time.time()),
                    )
                    self.connection.commit()
                return 503, receipt
            receipt = {"status": "running"}
            self.connection.execute(
                "INSERT INTO jobs(id,fingerprint,receipt,state,updated) VALUES(?,?,?,?,?)",
                (job_id, fingerprint, json.dumps(receipt), "running", time.time()),
            )
            self.connection.commit()
            cancel = threading.Event()
            self.active[job_id] = cancel
            self.pool.submit(self.execute, job_id, action, args, source, images, cancel)
            return 202, receipt

    def lookup(self, job_id: str) -> dict | None:
        with self.lock:
            self.cleanup()
            row = self.connection.execute("SELECT receipt FROM jobs WHERE id=?", (job_id,)).fetchone()
            return self.load_receipt(job_id, row[0]) if row else None

    def load_receipt(self, job_id: str, encoded: str) -> dict:
        snapshot = json.loads(encoded)
        snapshot.pop("delivered", None)
        output = snapshot.get("output")
        if output is None or "content_md5" in output:
            return snapshot
        # Older receipts can still be awaiting publication after a worker restart.
        try:
            current = file_description(self.outputs / job_id, output["name"])
        except (OSError, OfficeError):
            return snapshot
        if current["sha256"] == output["sha256"] and current["bytes"] == output["bytes"]:
            output["content_md5"] = current["content_md5"]
            self.connection.execute("UPDATE jobs SET receipt=? WHERE id=?", (json.dumps(snapshot, ensure_ascii=False), job_id))
            self.connection.commit()
        return snapshot

    def cancel(self, job_id: str) -> dict | None:
        with self.lock:
            snapshot = self.lookup(job_id)
            if snapshot is None:
                snapshot = {"status": "cancelled"}
                self.connection.execute(
                    "INSERT INTO jobs(id,fingerprint,receipt,state,updated) VALUES(?,?,?,?,?)",
                    (job_id, "cancelled-before-dispatch", json.dumps(snapshot), "cancelled", time.time()),
                )
                self.connection.commit()
                return snapshot
            if snapshot["status"] in ("completed", "partial") and not self.output_delivered(job_id):
                (self.outputs / job_id).unlink(missing_ok=True)
            elif snapshot["status"] != "running":
                return snapshot
            signal = self.active.get(job_id)
            if signal is not None:
                signal.set()
                self.execution_slots.notify_all()
            snapshot = {"status": "cancelled"}
            self.save(job_id, snapshot)
            return snapshot

    def save(self, job_id: str, snapshot: dict) -> None:
        self.connection.execute(
            "UPDATE jobs SET receipt=?,state=?,updated=? WHERE id=?",
            (json.dumps(snapshot, ensure_ascii=False, separators=(",", ":")), snapshot["status"], time.time(), job_id),
        )
        self.connection.commit()

    def source_file(self, source: InputFile, path: Path, cancelled: threading.Event, deadline: float) -> Path:
        key = f"{source.version_id}-{source.sha256}"
        cached = self.cache / key
        lock = self.download_locks[int(source.sha256[:2], 16) % len(self.download_locks)]
        with lock:
            if cancelled.is_set() or time.monotonic() >= deadline:
                raise OfficeError("文件读取已停止。", "JOB_TIMED_OUT")
            try:
                os.link(cached, path)
            except FileNotFoundError:
                pass
            else:
                try:
                    os.utime(cached)
                except FileNotFoundError:
                    pass
                if path.stat().st_size != source.bytes:
                    raise OfficeError("缓存文件与指定版本不一致。", "INPUT_HASH_MISMATCH")
                return path
            size = 0
            digest = hashlib.sha256()
            try:
                with DOWNLOAD.open(source.url, timeout=10) as remote, path.open("wb") as handle:
                    while chunk := remote.read(64 * 1024):
                        if cancelled.is_set() or time.monotonic() >= deadline:
                            raise OfficeError("文件读取已停止。", "JOB_TIMED_OUT")
                        size += len(chunk)
                        if size > source.bytes:
                            raise OfficeError("存储文件长度与指定版本不一致。", "INPUT_HASH_MISMATCH")
                        digest.update(chunk)
                        handle.write(chunk)
            except (HTTPError, URLError, TimeoutError) as exc:
                if isinstance(exc, HTTPError):
                    exc.close()
                raise OfficeError("无法读取存储中的输入文件。", "INPUT_UNAVAILABLE") from exc
            if size != source.bytes or digest.hexdigest() != source.sha256:
                raise OfficeError("存储文件与指定版本不一致。", "INPUT_HASH_MISMATCH")
            try:
                os.link(path, cached)
            except FileExistsError:
                pass
            return path

    def execute(self, job_id: str, action: str, args: dict, source: InputFile | None, images: list[InputFile], cancelled: threading.Event):
        try:
            with self.execution_slots:
                self.execution_slots.wait_for(lambda: cancelled.is_set() or len(self.executing) < self.limit)
                if cancelled.is_set():
                    return
                self.executing.add(job_id)
            deadline = time.monotonic() + JOB_TIMEOUT
            with tempfile.TemporaryDirectory(dir=self.temporary) as temporary:
                directory = Path(temporary)
                snapshot, stage_output = self.run_operation(directory, action, args, source, images, cancelled, deadline)
                with self.lock:
                    if cancelled.is_set():
                        return
                    if stage_output:
                        stage_output.replace(self.outputs / job_id)
                    self.save(job_id, snapshot)
        except OfficeError as exc:
            with self.lock:
                if not cancelled.is_set():
                    self.save(job_id, self.failure(exc.code, str(exc)))
        except Exception:
            LOG.error("Office job %s failed to finish", job_id)
            with self.lock:
                if not cancelled.is_set():
                    self.save(job_id, self.failure("WORKER_FAILED", "办公节点没有完成文件处理。"))
        finally:
            with self.execution_slots:
                self.executing.discard(job_id)
                self.active.pop(job_id, None)
                self.execution_slots.notify_all()

    def run_operation(self, directory: Path, action: str, args: dict, source: InputFile | None, images: list[InputFile], cancelled: threading.Event, deadline: float):
        path = self.source_file(source, directory / "input", cancelled, deadline) if source else None
        request = {"action": action, "args": args, "source": str(path) if path else None, "input_name": source.name if source else ""}
        if images:
            request["images"] = {}
            for image in images:
                name = f"image-{image.version_id}{Path(image.name).suffix.lower()}"
                self.source_file(image, directory / name, cancelled, deadline)
                request["images"][image.version_id] = name
        (directory / "request.json").write_text(json.dumps(request), encoding="utf-8")
        app_path = Path(__file__).resolve().parent.parent
        env = {"PYTHONPATH": str(app_path), "PYTHONDONTWRITEBYTECODE": "1", "LANG": "C.UTF-8", "PATH": "/usr/bin:/bin", "OPENBLAS_NUM_THREADS": "1"}
        with subprocess.Popen(
            [sys.executable, "-m", "workbench_office_tools.runner"],
            cwd=directory, env=env, stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, start_new_session=True,
        ) as process:
            while process.poll() is None:
                if cancelled.wait(0.05) or time.monotonic() >= deadline:
                    try:
                        os.killpg(process.pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                    process.wait()
                    return self.failure("JOB_TIMED_OUT", "办公文件处理超时。"), None
        receipt = directory / "receipt.json"
        if process.returncode or not receipt.is_file() or receipt.stat().st_size > 384 * 1024:
            return self.failure("WORKER_FAILED", "办公文件解析失败或超出执行资源。"), None
        snapshot = json.loads(receipt.read_text(encoding="utf-8"))
        name = snapshot.pop("name", None)
        output = None
        if name and snapshot["status"] in ("completed", "partial"):
            output = directory / "output"
            snapshot["output"] = file_description(output, name)
        return snapshot, output

    def upload_result(self, job_id: str, payload: dict) -> tuple[int, dict]:
        if not isinstance(payload, dict):
            raise OfficeError("文件上传参数无效。", "INVALID_ARGUMENT")
        url, headers = payload.get("url"), payload.get("headers") or {}
        version_id = payload.get("version_id")
        if version_id is not None and (not isinstance(version_id, str) or not VERSION.fullmatch(version_id)):
            raise OfficeError("文件版本无效。", "INVALID_ARGUMENT")
        if not isinstance(url, str) or len(url) > 8192 or origin(url) not in self.storage_origins:
            raise OfficeError("文件地址不在配置的存储范围内。", "OUTPUT_UNAVAILABLE")
        if not isinstance(headers, dict):
            raise OfficeError("文件上传参数无效。", "INVALID_ARGUMENT")
        allowed = {"content-type", "content-md5", "if-none-match"}
        signed = {}
        for key, value in headers.items():
            header = key.lower()
            if (
                header not in allowed or header in signed or not isinstance(value, str) or len(value) > 512
                or any(ord(ch) < 32 or ord(ch) >= 127 for ch in value)
            ):
                raise OfficeError("文件上传参数无效。", "INVALID_ARGUMENT")
            signed[header] = value
        with self.lock:
            snapshot = self.lookup(job_id)
            if snapshot is None:
                return 404, self.failure("JOB_NOT_FOUND", "未找到办公任务。")
            if snapshot["status"] not in ("completed", "partial") or "output" not in snapshot:
                return 409, self.failure("OUTPUT_UNAVAILABLE", "办公任务暂未生成可交付文件。")
            checksum = snapshot["output"].get("content_md5")
            if not checksum or signed.get("content-md5") != checksum:
                raise OfficeError("成品校验值与上传签名不一致。", "OUTPUT_CHECKSUM_MISMATCH")
            output_path = self.outputs / job_id
            try:
                handle = output_path.open("rb")
            except FileNotFoundError:
                return 404, self.failure("OUTPUT_EXPIRED", "办公结果已过期。")
        with handle:
            signed["content-length"] = str(snapshot["output"]["bytes"])
            try:
                request = Request(url, data=handle, headers=signed, method="PUT")
                with DOWNLOAD.open(request, timeout=30):
                    pass
            except HTTPError as exc:
                exc.close()
                if exc.code == 412:
                    self.mark_delivered(job_id)
                    return 412, self.failure("OUTPUT_ALREADY_EXISTS", "成品对象已存在。")
                LOG.warning("Storage rejected output upload for job %s with HTTP %s", job_id, exc.code)
                return 502, self.failure("OUTPUT_UPLOAD_FAILED", "成品上传到存储时失败。")
            except (URLError, TimeoutError):
                return 502, self.failure("OUTPUT_UPLOAD_FAILED", "成品上传到存储时失败。")
            if version_id is not None:
                self.cache_uploaded_output(output_path, handle, version_id, snapshot["output"])
        self.mark_delivered(job_id)
        return 200, snapshot

    def output_delivered(self, job_id: str) -> bool:
        row = self.connection.execute("SELECT receipt FROM jobs WHERE id=?", (job_id,)).fetchone()
        return bool(row and json.loads(row[0]).get("delivered"))

    def mark_delivered(self, job_id: str) -> None:
        with self.lock:
            if self.output_delivered(job_id):
                return
            snapshot = self.lookup(job_id)
            if snapshot and snapshot["status"] in ("completed", "partial"):
                snapshot["delivered"] = True
                self.save(job_id, snapshot)

    def cache_uploaded_output(self, path: Path, handle, version_id: str, output: dict) -> None:
        cached = self.cache / f"{version_id}-{output['sha256']}"
        with self.lock:
            try:
                uploaded = os.fstat(handle.fileno())
                # Cleanup can unlink a result while the open upload is still finishing.
                if uploaded.st_size != output["bytes"] or not os.path.samestat(path.stat(), uploaded):
                    return
                try:
                    os.link(path, cached)
                except FileExistsError:
                    pass
                os.utime(cached)
            except OSError:
                pass  # A subsequent operation can still read the committed object.

    def close(self):
        self.pool.shutdown(wait=True)
        self.connection.close()
        self.lockfile.close()


class OfficeHTTP(ThreadingHTTPServer):
    def __init__(self, address, service: JobService, token: str):
        super().__init__(address, Handler)
        self.jobs = service
        self.token = "Bearer " + token


class Handler(BaseHTTPRequestHandler):
    server: OfficeHTTP

    def setup(self):
        self.request.settimeout(15)
        super().setup()

    def do_GET(self):  # noqa: N802
        self.handle_job("GET")

    def do_POST(self):  # noqa: N802
        self.handle_job("POST")

    def do_PUT(self):  # noqa: N802
        self.handle_job("PUT")

    def do_DELETE(self):  # noqa: N802
        self.handle_job("DELETE")

    def log_message(self, _format, *_args):
        # Signed links and input arguments must never reach request logs.
        pass

    def send_json(self, status: int, snapshot: dict):
        encoded = json.dumps(snapshot, ensure_ascii=False).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(encoded)))
        self.send_header("X-Office-Node-ID", self.server.jobs.node_id)
        self.end_headers()
        self.wfile.write(encoded)

    def handle_job(self, method: str):
        if method == "GET" and self.path == "/health":
            self.send_json(200, {"status": "ok"})
            return
        supplied = self.headers.get("Authorization", "")
        if not hmac.compare_digest(supplied.encode(), self.server.token.encode()):
            self.send_json(401, JobService.failure("UNAUTHORIZED", "办公服务凭证无效。"))
            return
        expected_node = self.headers.get("X-Office-Node-ID")
        if expected_node and expected_node != self.server.jobs.node_id:
            self.send_json(409, JobService.failure("NODE_ID_MISMATCH", "办公节点身份已变更。"))
            return
        if method == "GET" and self.path == "/v1/ready":
            self.send_json(200, {"status": "ready"})
            return
        if method == "GET" and self.path == "/v1/status":
            self.send_json(200, self.server.jobs.status())
            return
        if method == "PUT":
            self.handle_concurrency_update(expected_node)
            return
        match = JOB_PATH.fullmatch(self.path)
        allowed_method = {"/output": "GET", "/upload": "POST"}
        if match is None or match[2] and method != allowed_method[match[2]]:
            self.send_json(404, JobService.failure("JOB_NOT_FOUND", "未找到办公任务。"))
            return
        job_id = match[1]
        code = 200
        try:
            if method == "POST":
                payload = self.read_payload()
                if match[2] == "/upload":
                    code, state = self.server.jobs.upload_result(job_id, payload)
                else:
                    code, state = self.server.jobs.start(job_id, payload, pooled=bool(expected_node))
            elif method == "DELETE":
                state = self.server.jobs.cancel(job_id)
            else:
                state = self.server.jobs.lookup(job_id)
                if match[2] == "/output" and state and state.get("output"):
                    self.send_output(job_id, state["output"])
                    return
                if match[2]:
                    state = None
            if state is None:
                code, state = 404, JobService.failure("JOB_NOT_FOUND", "未找到办公任务或结果。")
        except OfficeError as exc:
            code, state = 400, JobService.failure(exc.code, str(exc))
        except (ValueError, TypeError, UnicodeError):
            code, state = 400, JobService.failure("INVALID_ARGUMENT", "办公任务参数无效。")
        self.send_json(code, state)

    def read_payload(self) -> object:
        length = int(self.headers.get("Content-Length", "0"))
        if not 0 < length <= REQUEST_LIMIT or self.headers.get("Transfer-Encoding"):
            raise OfficeError("办公请求体超出处理范围。", "INVALID_ARGUMENT")
        return json.loads(self.rfile.read(length))

    def handle_concurrency_update(self, expected_node: str | None):
        if self.path != "/v1/concurrency":
            self.send_json(404, JobService.failure("JOB_NOT_FOUND", "未找到办公操作。"))
            return
        if not expected_node:
            self.send_json(409, JobService.failure("NODE_ID_MISMATCH", "更新并发需要指定办公节点身份。"))
            return
        try:
            status = self.server.jobs.configure_concurrency(self.read_payload())
        except OfficeError as exc:
            self.send_json(400, JobService.failure(exc.code, str(exc)))
        except (ValueError, TypeError, UnicodeError):
            self.send_json(400, JobService.failure("INVALID_ARGUMENT", "办公节点并发配置无效。"))
        else:
            self.send_json(200, status)

    def send_output(self, job_id: str, meta: dict):
        try:
            handle = (self.server.jobs.outputs / job_id).open("rb")
        except FileNotFoundError:
            self.send_json(404, JobService.failure("OUTPUT_EXPIRED", "办公结果已过期。"))
            return
        with handle:
            self.send_response(200)
            self.send_header("Content-Type", "application/octet-stream")
            self.send_header("Cache-Control", "no-store")
            self.send_header("Content-Length", str(meta["bytes"]))
            self.end_headers()
            shutil.copyfileobj(handle, self.wfile, 128 * 1024)


def main():
    logging.basicConfig(level=logging.INFO)
    os.umask(0o077)
    token = os.environ.get("OFFICE_TOOLS_TOKEN", "")
    if len(token) < 24:
        raise SystemExit("OFFICE_TOOLS_TOKEN must contain at least 24 characters")
    origins = {origin(value.strip()) for value in os.environ.get("OFFICE_TOOLS_STORAGE_ORIGINS", "").split(",") if value.strip()}
    if not origins:
        raise SystemExit("OFFICE_TOOLS_STORAGE_ORIGINS must contain an allowed storage origin")
    concurrency = int(os.environ.get("OFFICE_TOOLS_CONCURRENCY", "2"))
    if not 1 <= concurrency <= MAX_CONCURRENCY:
        raise SystemExit(f"OFFICE_TOOLS_CONCURRENCY must be between 1 and {MAX_CONCURRENCY}")
    directory = Path(os.environ.get("OFFICE_TOOLS_STATE_DIR", "/var/lib/office-tools"))
    host = os.environ.get("OFFICE_TOOLS_HOST", "0.0.0.0")
    port = int(os.environ.get("OFFICE_TOOLS_PORT", "8094"))
    service = JobService(directory, origins, concurrency, os.environ.get("OFFICE_TOOLS_RECALC_ENABLED") == "true")
    with OfficeHTTP((host, port), service, token) as server:
        LOG.info("Office tool service listening on port %s", port)
        try:
            server.serve_forever()
        finally:
            service.close()


if __name__ == "__main__":
    main()
