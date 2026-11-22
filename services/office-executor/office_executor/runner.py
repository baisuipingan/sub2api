"""Fixed image entrypoint. Never run this module on the application host.

This is NOT the isolation boundary. The supervisor must create a networkless,
resource-limited gVisor container before invoking it.
"""
from __future__ import annotations

import io
import json
import os
from pathlib import Path, PurePosixPath
import resource
import signal
import stat
import subprocess
import sys
import threading
import time
import zipfile

from .contracts import ContractError, PUBLIC_EXTENSIONS, decode_file, file_record, logical_path, public_output_metadata, validate_request


def check_public_type(path: str, content: bytes) -> None:
    if not path.startswith("output/"):
        return
    suffix = PurePosixPath(path).suffix.lower()
    if suffix not in PUBLIC_EXTENSIONS:
        raise ContractError("unsupported published output type")
    signatures = {".pdf": b"%PDF-", ".png": b"\x89PNG\r\n\x1a\n", ".jpg": b"\xff\xd8\xff", ".jpeg": b"\xff\xd8\xff"}
    if suffix in signatures and not content.startswith(signatures[suffix]):
        raise ContractError("published output signature mismatch")
    if suffix == ".webp" and not (content[:4] == b"RIFF" and content[8:12] == b"WEBP"):
        raise ContractError("published output signature mismatch")
    if suffix in {".docx", ".xlsx", ".pptx"}:
        required = {".docx": "word/document.xml", ".xlsx": "xl/workbook.xml", ".pptx": "ppt/presentation.xml"}[suffix]
        try:
            with zipfile.ZipFile(io.BytesIO(content)) as archive:
                entries = archive.infolist()
                names = {entry.filename for entry in entries}
                if len(entries) > 10000 or sum(entry.file_size for entry in entries) > 128 * 1024 * 1024:
                    raise ContractError("published Office archive exceeds unpack limits")
                if required not in names or "[Content_Types].xml" not in names:
                    raise ContractError("published Office archive is incomplete")
                if len(names) != len(entries):
                    raise ContractError("duplicate entries in published Office archive")
                content_types = archive.getinfo("[Content_Types].xml")
                if content_types.file_size > 1024 * 1024 or content_types.flag_bits & 1:
                    raise ContractError("published Office content metadata exceeds limit")
                metadata = archive.read(content_types).lower()
                if any(marker in metadata for marker in (b"macroenabled", b"vbaproject", b"macrosheet")):
                    raise ContractError("macro-enabled published Office content is forbidden")
                for entry in entries:
                    parts = PurePosixPath(entry.filename).parts
                    if entry.filename.startswith("/") or ".." in parts or "\\" in entry.filename or "vbaproject" in entry.filename.lower():
                        raise ContractError("unsafe published Office archive")
                    if stat.S_ISLNK(entry.external_attr >> 16):
                        raise ContractError("symlink inside published Office archive")
                    if entry.flag_bits & 1:
                        raise ContractError("encrypted published Office archive is forbidden")
        except (zipfile.BadZipFile, OSError, RuntimeError) as exc:
            raise ContractError("invalid published Office archive") from exc
    if suffix in {".txt", ".md", ".csv", ".json"}:
        try:
            decoded = content.decode("utf-8")
            if suffix == ".json":
                json.loads(decoded)
        except (UnicodeError, ValueError, RecursionError) as exc:
            raise ContractError("invalid published text output") from exc


def read_regular(root: Path, relative: str, maximum: int) -> bytes:
    """No-follow every component, including a maliciously replaced parent."""
    descriptor = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        parts = PurePosixPath(relative).parts
        for component in parts[:-1]:
            child = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=descriptor)
            os.close(descriptor)
            descriptor = child
        file_fd = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=descriptor)
        try:
            before = os.fstat(file_fd)
            if not stat.S_ISREG(before.st_mode) or before.st_nlink != 1 or before.st_size > maximum:
                raise ContractError("output must be a bounded regular file without hardlinks")
            chunks, size = [], 0
            while True:
                chunk = os.read(file_fd, min(64 * 1024, maximum - size + 1))
                if not chunk:
                    break
                size += len(chunk)
                if size > maximum:
                    raise ContractError("output exceeds byte limit")
                chunks.append(chunk)
            after = os.fstat(file_fd)
            if before.st_size != after.st_size or before.st_mtime_ns != after.st_mtime_ns:
                raise ContractError("output changed during collection")
            return b"".join(chunks)
        finally:
            os.close(file_fd)
    finally:
        os.close(descriptor)


def collect_workspace(root: Path, limits: dict) -> list[dict]:
    records, total = [], 0
    def walk_error(error):
        raise error
    for zone in ("output", "scratch"):
        base = root / zone
        if not base.is_dir() or base.is_symlink():
            raise ContractError("workspace zone was replaced")
        for directory, dirs, files in os.walk(base, followlinks=False, onerror=walk_error):
            # Runtime HOME/temp/cache aren't conversation resources.
            if directory == str(base) and zone == "scratch":
                dirs[:] = [name for name in dirs if name not in {".home", ".tmp", ".cache", ".lo-profile"}]
            for name in dirs:
                if (Path(directory) / name).is_symlink():
                    raise ContractError("symlink directories are forbidden in workspace results")
            for name in sorted(files):
                path = (Path(directory) / name).relative_to(root).as_posix()
                logical_path(path, writable=True)
                if len(records) >= limits["max_output_files"]:
                    raise ContractError("workspace output file limit exceeded")
                content = read_regular(root, path, limits["max_output_bytes"] - total)
                total += len(content)
                check_public_type(path, content)
                records.append(file_record(path, content))
    return sorted(records, key=lambda item: item["path"])


def _drain(pipe, target: bytearray, maximum: int) -> None:
    try:
        while chunk := pipe.read(8192):
            if len(target) < maximum:
                target.extend(chunk[:maximum - len(target)])
    finally:
        pipe.close()


def execute(request: dict, root: Path, request_dir: Path) -> dict:
    limits = request["limits"]
    for zone in ("output", "scratch"):
        (root / zone).mkdir(mode=0o700, exist_ok=True)
    for private in (".home", ".tmp", ".cache", ".lo-profile"):
        (root / "scratch" / private).mkdir(mode=0o700, exist_ok=True)
    if not (root / "skills").exists():
        (root / "skills").symlink_to("/opt/office-skills", target_is_directory=True)
    for item in request["inputs"]:
        if item["path"].startswith("uploads/"):
            continue  # uploads are mounted read-only by the supervisor.
        path = root / item["path"]
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(decode_file(item))
    script_file = request_dir / "script.py"
    environment = {
        "PATH": "/usr/local/bin:/usr/bin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8",
        "PYTHONIOENCODING": "utf-8", "PYTHONUNBUFFERED": "1", "PYTHONDONTWRITEBYTECODE": "1",
        "PYTHONPATH": "/opt/office-executor",
        "HOME": str(root / "scratch/.home"), "TMPDIR": str(root / "scratch/.tmp"),
        "XDG_CACHE_HOME": str(root / "scratch/.cache"), "XDG_CONFIG_HOME": str(root / "scratch/.home"),
        "MPLCONFIGDIR": str(root / "scratch/.cache/matplotlib"),
        "MATPLOTLIBRC": "/etc/matplotlibrc", "OPENBLAS_NUM_THREADS": "1", "OMP_NUM_THREADS": "1",
        "MKL_NUM_THREADS": "1", "NUMEXPR_NUM_THREADS": "1",
        "OFFICE_WORKSPACE": str(root), "OFFICE_LO_PROFILE": str(root / "scratch/.lo-profile"),
        "MPLBACKEND": "Agg", "TZ": "UTC",
    }
    def child_limits():
        resource.setrlimit(resource.RLIMIT_FSIZE, (limits["max_output_bytes"], limits["max_output_bytes"]))
        resource.setrlimit(resource.RLIMIT_NOFILE, (256, 256))
        resource.setrlimit(resource.RLIMIT_CPU, (limits["timeout_seconds"], limits["timeout_seconds"] + 1))
    started = time.monotonic()
    process = subprocess.Popen([sys.executable, "-I", str(script_file)], cwd=root, env=environment,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True,
                               preexec_fn=child_limits)
    stdout, stderr = bytearray(), bytearray()
    threads = [threading.Thread(target=_drain, args=(pipe, buffer, limits["max_log_bytes"]), daemon=True)
               for pipe, buffer in ((process.stdout, stdout), (process.stderr, stderr))]
    for thread in threads:
        thread.start()
    status = "completed"
    try:
        process.wait(timeout=limits["timeout_seconds"])
    except subprocess.TimeoutExpired:
        status = "timed_out"
    finally:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.wait(timeout=3)
        for thread in threads:
            thread.join(timeout=1)
    if process.returncode != 0 and status == "completed":
        status = "failed"
    result = {"version": 1, "run_id": request["run_id"], "operation_id": request["operation_id"],
              "lease_epoch": request["lease_epoch"], "status": status, "exit_code": process.returncode,
              "stdout": stdout.decode("utf-8", "replace"), "stderr": stderr.decode("utf-8", "replace"),
              "duration_ms": int((time.monotonic() - started) * 1000), "workspace_manifest": [], "outputs": []}
    if status != "timed_out":
        try:
            snapshot = collect_workspace(root, limits)
            result["workspace_manifest"] = snapshot
            result["outputs"] = public_output_metadata(snapshot)
        except (ContractError, OSError) as exc:
            result.update(status="invalid_output", error_code="INVALID_WORKSPACE_RESULT", error=str(exc))
    return result


def main() -> None:
    # Guard against mistakenly invoking an uncontained runner during development.
    if not Path("/run/office-input/request.json").is_file() or os.getuid() != 65532:
        raise SystemExit("office runner requires the fixed task container contract")
    request_dir = Path("/run/office-input")
    request = validate_request(json.loads((request_dir / "request.json").read_text()))
    result = execute(request, Path("/workspace"), request_dir)
    write_result(result)


def write_result(result: dict) -> None:
    # The fixed image uses unbuffered stdout. Raw writes may be short (gVisor
    # pipes return at most one buffer), while TextIOWrapper.write does not
    # guarantee retrying a large raw FileIO write. Account for every byte so
    # large Office snapshots cannot become a silently truncated JSON result.
    encoded = memoryview(json.dumps(result, ensure_ascii=True, separators=(",", ":")).encode("ascii"))
    sys.stdout.flush()
    while encoded:
        try:
            written = os.write(sys.stdout.fileno(), encoded[:32 * 1024])
        except InterruptedError:
            continue
        if written <= 0:
            raise OSError("runner result stream closed before completion")
        encoded = encoded[written:]


if __name__ == "__main__":
    main()
