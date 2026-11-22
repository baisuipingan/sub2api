"""Trusted Linux-only Docker/gVisor supervisor; never executes model code itself."""
from __future__ import annotations

from dataclasses import dataclass, field
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import sqlite3
import subprocess
import tempfile
import threading
import time
import re

from .contracts import ContractError, DEFAULT_LIMITS, MAX_RESULT_WIRE_BYTES, MIB, decode_file, file_record, logical_path, public_output_metadata, request_digest, validate_request
from .runner import check_public_type


class ExecutionError(RuntimeError):
    def __init__(self, code: str, message: str, http_status: int = 503):
        super().__init__(message)
        self.code, self.http_status = code, http_status


@dataclass(frozen=True)
class Settings:
    image: str
    state_dir: Path
    docker: str = "docker"
    concurrency: int = 2
    result_ttl_seconds: int = 3600
    state_max_bytes: int = 1024 * MIB
    minimum_free_bytes: int = 256 * MIB
    limits: dict = field(default_factory=lambda: dict(DEFAULT_LIMITS))

    def __post_init__(self):
        immutable = self.image.startswith("sha256:") and len(self.image) == 71
        immutable |= "@sha256:" in self.image and len(self.image.rsplit("@sha256:", 1)[1]) == 64
        digest = self.image.rsplit("sha256:", 1)[-1]
        if not immutable or any(char not in "0123456789abcdef" for char in digest):
            raise ValueError("OFFICE_EXECUTOR_IMAGE must be an immutable image digest or local image ID")
        if not 1 <= self.concurrency <= 8:
            raise ValueError("executor concurrency must be between 1 and 8")
        if not self.state_dir.is_absolute() or any(char in str(self.state_dir) for char in (",", "\n", "\x00")):
            raise ValueError("state directory must be an absolute Docker-compatible bind path")


def docker_prefix(settings: Settings) -> list[str]:
    # Docker CLI otherwise auto-injects proxy credentials from ~/.docker/config.json.
    # This service never pulls images, never needs registry auth, and only uses
    # the local dedicated daemon. Do not inherit a developer's Docker context.
    return [settings.docker, "--config", str(settings.state_dir / ".docker-config"),
            "--host", "unix:///var/run/docker.sock"]


def container_command(settings: Settings, request: dict, stage: Path, name: str) -> list[str]:
    limit = request["limits"]
    return [*docker_prefix(settings), "create", "--pull=never", "--name", name,
            "--label", "milo.office-executor=1",
            "--label", "milo.office-executor.namespace=" + hashlib.sha256(str(settings.state_dir).encode()).hexdigest()[:16],
            "--runtime=runsc", "--network=none", "--ipc=none",
            "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges:true",
            "--user=65532:65532", f"--pids-limit={limit['pids']}",
            f"--memory={limit['memory_mb']}m", f"--memory-swap={limit['memory_mb']}m",
            f"--cpus={limit['cpus']}", "--ulimit=nofile=256:256", "--log-driver=none",
            "--workdir=/workspace", "--hostname=office-task",
            "--tmpfs", f"/workspace:rw,noexec,nosuid,nodev,size={limit['disk_mb']}m,uid=65532,gid=65532,mode=0700",
            "--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=32m,uid=65532,gid=65532,mode=0700",
            "--mount", f"type=bind,src={stage / 'request'},dst=/run/office-input,readonly",
            "--mount", f"type=bind,src={stage / 'uploads'},dst=/workspace/uploads,readonly",
            settings.image]


def _bounded_capture(command: list[str], maximum: int, timeout: float, cancel: threading.Event) -> tuple[int, bytes, bytes, str | None]:
    process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    buffers = [bytearray(), bytearray()]
    overflow = threading.Event()
    def drain(index: int, pipe):
        try:
            while chunk := pipe.read(64 * 1024):
                remaining = maximum - len(buffers[index])
                if len(chunk) > remaining:
                    overflow.set()
                buffers[index].extend(chunk[:max(remaining, 0)])
        finally:
            pipe.close()
    readers = [threading.Thread(target=drain, args=(index, pipe), daemon=True)
               for index, pipe in enumerate((process.stdout, process.stderr))]
    for reader in readers:
        reader.start()
    deadline, reason = time.monotonic() + timeout, None
    while process.poll() is None:
        if overflow.is_set() or cancel.is_set() or time.monotonic() >= deadline:
            reason = "output_limit" if overflow.is_set() else "cancelled" if cancel.is_set() else "timed_out"
            process.kill()
            break
        cancel.wait(0.05)
    process.wait(timeout=5)
    for reader in readers:
        reader.join(timeout=2)
    if overflow.is_set() and reason is None:
        reason = "output_limit"
    return process.returncode, bytes(buffers[0]), bytes(buffers[1]), reason


class Supervisor:
    def __init__(self, settings: Settings):
        self.settings = settings
        settings.state_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
        docker_config = settings.state_dir / ".docker-config"
        docker_config.mkdir(mode=0o700, exist_ok=True)
        (docker_config / "config.json").write_text('{"auths":{}}')
        (docker_config / "config.json").chmod(0o600)
        self._slots = threading.BoundedSemaphore(settings.concurrency)
        self._active: dict[tuple[str, str], threading.Event] = {}
        self._lock = threading.Lock()
        self._unhealthy = threading.Event()
        with self._db() as db:
            db.executescript("""
                CREATE TABLE IF NOT EXISTS operations (
                    run_id TEXT NOT NULL, operation_id TEXT NOT NULL, epoch INTEGER NOT NULL,
                    digest TEXT NOT NULL, status TEXT NOT NULL, result_path TEXT,
                    updated REAL NOT NULL, PRIMARY KEY(run_id, operation_id));
                CREATE TABLE IF NOT EXISTS runs (run_id TEXT PRIMARY KEY, epoch INTEGER NOT NULL);
            """)
            # A crashed supervisor cannot prove a model tool's final outcome. Never replay.
            db.execute("UPDATE operations SET status='unknown' WHERE status='running'")

    @contextmanager
    def _db(self):
        connection = sqlite3.connect(self.settings.state_dir / "journal.sqlite", timeout=10)
        try:
            connection.execute("PRAGMA journal_mode=WAL")
            with connection:
                yield connection
        finally:
            connection.close()

    def doctor(self) -> dict:
        if self._unhealthy.is_set():
            raise ExecutionError("CLEANUP_REQUIRED", "task cleanup failed; executor requires operator recovery")
        if os.uname().sysname != "Linux":
            raise ExecutionError("UNSUPPORTED_HOST", "office execution requires a Linux host with gVisor")
        try:
            runtimes = subprocess.run([*docker_prefix(self.settings), "info", "--format", "{{json .Runtimes}}"],
                                      capture_output=True, timeout=10, check=True)
            if "runsc" not in json.loads(runtimes.stdout):
                raise ExecutionError("GVISOR_UNAVAILABLE", "runsc is required; no standard-container fallback is permitted")
            subprocess.run([*docker_prefix(self.settings), "image", "inspect", self.settings.image],
                           capture_output=True, timeout=10, check=True)
        except (OSError, subprocess.SubprocessError, ValueError) as exc:
            raise ExecutionError("EXECUTOR_UNAVAILABLE", "Docker, gVisor, or the pinned local task image is unavailable") from exc
        return {"status": "ready", "runtime": "runsc", "network": "none", "image": self.settings.image}

    def recover(self) -> None:
        """Under the CLI's singleton lock, kill this supervisor's abandoned tasks."""
        namespace = hashlib.sha256(str(self.settings.state_dir).encode()).hexdigest()[:16]
        try:
            result = subprocess.run([*docker_prefix(self.settings), "ps", "-aq", "--filter",
                                     "label=milo.office-executor.namespace=" + namespace],
                                    check=True, capture_output=True, timeout=15)
            identifiers = result.stdout.decode().split()
            if any(not re.fullmatch(r"[0-9a-f]{12,64}", value) for value in identifiers):
                raise ExecutionError("RECOVERY_FAILED", "invalid abandoned-container inventory")
            for identifier in identifiers:
                subprocess.run([*docker_prefix(self.settings), "rm", "--force", identifier],
                               check=True, capture_output=True, timeout=15)
        except (OSError, subprocess.SubprocessError) as exc:
            raise ExecutionError("RECOVERY_FAILED", "abandoned task cleanup failed; executor remains unavailable") from exc
        for path in self.settings.state_dir.iterdir():
            if path.name.startswith("task-"):
                if path.is_dir() and not path.is_symlink():
                    shutil.rmtree(path)
                else:
                    path.unlink()
            elif path.suffix == ".tmp":
                path.unlink()
        self.cleanup()

    def cleanup(self) -> None:
        with self._lock, self._db() as db:
            old = db.execute("SELECT result_path FROM operations WHERE result_path IS NOT NULL AND updated < ?",
                             (time.time() - self.settings.result_ttl_seconds,)).fetchall()
            for (relative,) in old:
                (self.settings.state_dir / relative).unlink(missing_ok=True)
            db.execute("UPDATE operations SET result_path=NULL, status='result_expired' WHERE result_path IS NOT NULL AND updated < ?",
                       (time.time() - self.settings.result_ttl_seconds,))

    def _claim(self, request: dict) -> dict | None:
        digest = request_digest(request)
        with self._lock, self._db() as db:
            db.execute("BEGIN IMMEDIATE")
            row = db.execute("SELECT digest,status,result_path FROM operations WHERE run_id=? AND operation_id=?",
                             (request["run_id"], request["operation_id"])).fetchone()
            if row:
                if row[0] != digest:
                    raise ExecutionError("OPERATION_CONFLICT", "operation ID already belongs to different input", 409)
                if row[2]:
                    try:
                        return json.loads((self.settings.state_dir / row[2]).read_bytes())
                    except (OSError, ValueError) as exc:
                        raise ExecutionError("RESULT_UNAVAILABLE", "stored operation result is unavailable; do not replay", 409) from exc
                raise ExecutionError("OPERATION_" + row[1].upper(), "operation already recorded; execution will not be replayed", 409)
            current = db.execute("SELECT epoch FROM runs WHERE run_id=?", (request["run_id"],)).fetchone()
            if current and request["lease_epoch"] < current[0]:
                raise ExecutionError("STALE_LEASE", "run lease epoch is stale", 409)
            active = db.execute("SELECT 1 FROM operations WHERE run_id=? AND status='running'", (request["run_id"],)).fetchone()
            if active:
                raise ExecutionError("RUN_BUSY", "run already has an active tool operation", 409)
            # Reserve room before creating a new journal operation. A rejected
            # request must not itself keep adding result files to a full disk.
            # Reservation covers input staging + bounded JSON/base64 result;
            # count active reservations too (conservative when already staged).
            active_count = db.execute("SELECT COUNT(*) FROM operations WHERE status='running'").fetchone()[0]
            self._check_capacity(active_count + 1)
            db.execute("INSERT INTO runs VALUES (?,?) ON CONFLICT(run_id) DO UPDATE SET epoch=excluded.epoch",
                       (request["run_id"], request["lease_epoch"]))
            db.execute("INSERT INTO operations VALUES (?,?,?,?, 'running', NULL, ?)",
                       (request["run_id"], request["operation_id"], request["lease_epoch"], digest, time.time()))
        return None

    def _check_capacity(self, reservations: int) -> None:
        usage = 0
        for directory, _, files in os.walk(self.settings.state_dir):
            for name in files:
                try:
                    usage += (Path(directory) / name).stat(follow_symlinks=False).st_size
                except FileNotFoundError:
                    pass  # Another completed operation just removed staging.
        reserved = reservations * 192 * MIB
        if usage + reserved > self.settings.state_max_bytes or shutil.disk_usage(self.settings.state_dir).free < self.settings.minimum_free_bytes + reserved:
            raise ExecutionError("EXECUTOR_STORAGE_FULL", "office execution state capacity is full", 503)

    def _finish(self, request: dict, result: dict) -> None:
        filename = hashlib.sha256((request["run_id"] + "/" + request["operation_id"]).encode()).hexdigest() + ".json"
        path = self.settings.state_dir / filename
        temporary = path.with_suffix(".tmp")
        with temporary.open("xb") as file:
            file.write(json.dumps(result, ensure_ascii=True, separators=(",", ":")).encode())
            file.flush()
            os.fsync(file.fileno())
        temporary.replace(path)
        with self._db() as db:
            db.execute("UPDATE operations SET status=?,result_path=?,updated=? WHERE run_id=? AND operation_id=?",
                       (result["status"], filename, time.time(), request["run_id"], request["operation_id"]))

    def cancel(self, run_id: str, operation_id: str) -> bool:
        with self._lock:
            event = self._active.get((run_id, operation_id))
            if event:
                event.set()
            return event is not None

    def execute(self, data: dict) -> dict:
        request = validate_request(data, self.settings.limits)
        if self._unhealthy.is_set():
            raise ExecutionError("CLEANUP_REQUIRED", "task cleanup failed; executor requires operator recovery")
        if not self._slots.acquire(blocking=False):
            raise ExecutionError("EXECUTOR_BUSY", "office worker capacity is full", 429)
        try:
            self.cleanup()
            replay = self._claim(request)
            if replay is not None:
                return replay
            event = threading.Event()
            key = request["run_id"], request["operation_id"]
            with self._lock:
                self._active[key] = event
            try:
                result = self._execute_container(request, event)
            except Exception:
                result = self._error_result(request, "unknown", "EXECUTION_OUTCOME_UNKNOWN")
                self._finish(request, result)
                raise
            finally:
                with self._lock:
                    self._active.pop(key, None)
            self._finish(request, result)
            return result
        finally:
            self._slots.release()

    @staticmethod
    def _error_result(request: dict, status: str, error_code: str) -> dict:
        return {"version": 1, "run_id": request["run_id"], "operation_id": request["operation_id"],
                "lease_epoch": request["lease_epoch"], "status": status, "error_code": error_code,
                "exit_code": None, "stdout": "", "stderr": "", "duration_ms": 0,
                "workspace_manifest": [], "outputs": []}

    def _validate_result(self, request: dict, data: object) -> dict:
        if not isinstance(data, dict):
            raise ContractError("invalid runner result")
        if any(data.get(key) != request[key] for key in ("run_id", "operation_id", "lease_epoch", "version")):
            raise ContractError("runner identity mismatch")
        if data.get("status") not in {"completed", "failed", "timed_out", "invalid_output"}:
            raise ContractError("invalid runner status")
        snapshot = data.get("workspace_manifest")
        if not isinstance(snapshot, list) or len(snapshot) > request["limits"]["max_output_files"]:
            raise ContractError("invalid runner workspace manifest")
        total, paths, normalized = 0, set(), []
        for item in snapshot:
            if not isinstance(item, dict):
                raise ContractError("invalid runner file")
            path = logical_path(item.get("path"), writable=True)
            if path in paths or any(path.startswith(old + "/") or old.startswith(path + "/") for old in paths):
                raise ContractError("duplicate or conflicting runner file")
            paths.add(path)
            content = decode_file(item)
            total += len(content)
            if total > request["limits"]["max_output_bytes"]:
                raise ContractError("runner output exceeds byte limit")
            check_public_type(path, content)
            normalized.append(file_record(path, content))
        for key in ("stdout", "stderr"):
            if not isinstance(data.get(key), str) or len(data[key].encode()) > request["limits"]["max_log_bytes"] * 3:
                raise ContractError("runner log exceeds limit")
        if type(data.get("exit_code")) is not int or type(data.get("duration_ms")) is not int:
            raise ContractError("invalid runner execution metadata")
        # Never trust a second, potentially contradictory set of output records.
        data["workspace_manifest"] = normalized
        data["outputs"] = public_output_metadata(normalized)
        return data

    def _execute_container(self, request: dict, cancel: threading.Event) -> dict:
        self.doctor()
        name = "milo-office-" + hashlib.sha256((str(self.settings.state_dir) + "/" + request["run_id"] + "/" + request["operation_id"]).encode()).hexdigest()[:32]
        with tempfile.TemporaryDirectory(prefix="task-", dir=self.settings.state_dir) as temporary:
            stage = Path(temporary)
            (stage / "request").mkdir(mode=0o755)
            (stage / "uploads").mkdir(mode=0o755)
            (stage / "request/request.json").write_text(json.dumps(request))
            (stage / "request/script.py").write_text(request["script"])
            for path in (stage / "request").iterdir():
                path.chmod(0o444)
            for item in request["inputs"]:
                if item["path"].startswith("uploads/"):
                    path = stage / item["path"]
                    path.parent.mkdir(mode=0o755, parents=True, exist_ok=True)
                    path.write_bytes(decode_file(item))
                    path.chmod(0o444)
            attempted = False
            try:
                attempted = True
                subprocess.run(container_command(self.settings, request, stage, name), check=True,
                               capture_output=True, timeout=20)
                if cancel.is_set():
                    return self._error_result(request, "cancelled", "EXECUTION_CANCELLED")
                # One base64 snapshot; outputs contains metadata only. This cap
                # matches the office-agent HTTP client, including JSON/logs.
                code, stdout, _, reason = _bounded_capture([*docker_prefix(self.settings), "start", "--attach", name], MAX_RESULT_WIRE_BYTES,
                                                         request["limits"]["timeout_seconds"] + 15, cancel)
                if reason:
                    return self._error_result(request, reason, "EXECUTION_" + reason.upper())
                if code != 0:
                    # No verified runner snapshot (e.g. OOM/entrypoint crash).
                    # An empty manifest here must not erase the previous
                    # workspace and invite the graph to regenerate files.
                    return self._error_result(request, "unknown", "TASK_CONTAINER_FAILED")
                try:
                    return self._validate_result(request, json.loads(stdout))
                except (ValueError, OSError, RecursionError):
                    return self._error_result(request, "invalid_output", "INVALID_RUNNER_RESULT")
            finally:
                if attempted:
                    try:
                        removed = subprocess.run([*docker_prefix(self.settings), "rm", "--force", name],
                                                 capture_output=True, timeout=15)
                        if removed.returncode != 0:
                            message = removed.stderr.decode("utf-8", "replace").lower()
                            if "no such container:" not in message and "no such object:" not in message:
                                raise ExecutionError("CLEANUP_FAILED", "task container removal could not be verified")
                    except (OSError, subprocess.SubprocessError) as exc:
                        self._unhealthy.set()
                        raise ExecutionError("CLEANUP_FAILED", "task container cleanup failed; operator attention required") from exc
                    except ExecutionError:
                        self._unhealthy.set()
                        raise
