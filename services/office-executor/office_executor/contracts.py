"""Wire validation shared by the supervisor and its fixed container entrypoint."""
from __future__ import annotations

import base64
import binascii
import hashlib
import json
import re
from pathlib import PurePosixPath

MIB = 1024 * 1024
MAX_INPUT_BYTES = 24 * MIB
MAX_SCRIPT_BYTES = 256 * 1024
MAX_FILES = 128
MAX_RESULT_WIRE_BYTES = 40 * MIB
ID = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_-]{0,95}$")
DIGEST = re.compile(r"^[0-9a-f]{64}$")
DEFAULT_LIMITS = {
    "timeout_seconds": 180, "memory_mb": 2048, "cpus": 1.0,
    "disk_mb": 256, "pids": 128, "max_output_bytes": 24 * MIB,
    "max_output_files": 128, "max_log_bytes": 64 * 1024,
}
MIN_LIMITS = {
    "timeout_seconds": 1, "memory_mb": 256, "cpus": 0.1, "disk_mb": 16,
    "pids": 16, "max_output_bytes": 1, "max_output_files": 1,
    "max_log_bytes": 256,
}
PUBLIC_EXTENSIONS = {
    ".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
    ".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
    ".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
    ".pdf": "application/pdf", ".png": "image/png", ".jpg": "image/jpeg",
    ".jpeg": "image/jpeg", ".webp": "image/webp", ".txt": "text/plain",
    ".md": "text/markdown", ".csv": "text/csv", ".json": "application/json",
}


class ContractError(ValueError):
    pass


def logical_path(value: object, *, writable: bool = False) -> str:
    if not isinstance(value, str):
        raise ContractError("invalid workspace path")
    try:
        length = len(value.encode())
    except UnicodeError as exc:
        raise ContractError("invalid workspace path encoding") from exc
    if length > 768:
        raise ContractError("invalid workspace path")
    if any(ord(c) < 32 for c in value) or "\\" in value or value.startswith("/"):
        raise ContractError("invalid workspace path")
    path = PurePosixPath(value)
    zones = {"output", "scratch"} if writable else {"uploads", "output", "scratch"}
    # Reject normalization (double slash, dot components, trailing slash) as well.
    if len(path.parts) < 2 or path.parts[0] not in zones or ".." in path.parts or str(path) != value:
        raise ContractError("path must name a file within a permitted workspace zone")
    if len(path.parts) > 12:
        raise ContractError("workspace path is too deep")
    return value


def decode_file(item: dict) -> bytes:
    if set(item) - {"path", "content_base64", "sha256", "size", "mime"}:
        raise ContractError("unknown file fields")
    encoded = item.get("content_base64")
    if not isinstance(encoded, str) or len(encoded) > (MAX_INPUT_BYTES + 2) // 3 * 4:
        raise ContractError("file content is too large")
    try:
        content = base64.b64decode(encoded, validate=True)
    except (ValueError, binascii.Error) as exc:
        raise ContractError("invalid file base64") from exc
    digest = item.get("sha256")
    if not isinstance(digest, str) or not DIGEST.fullmatch(digest) or hashlib.sha256(content).hexdigest() != digest:
        raise ContractError("file sha256 mismatch")
    if "size" in item and (type(item["size"]) is not int or item["size"] != len(content)):
        raise ContractError("file size mismatch")
    return content


def validate_request(data: object, ceilings: dict | None = None) -> dict:
    if not isinstance(data, dict):
        raise ContractError("request must be an object")
    expected = {"version", "run_id", "operation_id", "lease_epoch", "script", "inputs", "limits"}
    if set(data) - expected:
        raise ContractError("unknown request fields")
    if type(data.get("version")) is not int or data["version"] != 1:
        raise ContractError("unsupported contract version")
    for name in ("run_id", "operation_id"):
        if not isinstance(data.get(name), str) or not ID.fullmatch(data[name]):
            raise ContractError(f"invalid {name}")
    if type(data.get("lease_epoch")) is not int or not 1 <= data["lease_epoch"] < 2**63:
        raise ContractError("lease_epoch must be a positive integer")
    script = data.get("script")
    if not isinstance(script, str) or not script.strip() or "\x00" in script:
        raise ContractError("invalid or oversized Python script")
    try:
        if len(script.encode()) > MAX_SCRIPT_BYTES:
            raise ContractError("invalid or oversized Python script")
    except UnicodeError as exc:
        raise ContractError("invalid Python script encoding") from exc
    files = data.get("inputs", [])
    if not isinstance(files, list) or len(files) > MAX_FILES:
        raise ContractError("too many input files")
    total, seen = 0, set()
    for item in files:
        if not isinstance(item, dict):
            raise ContractError("invalid input file")
        name = logical_path(item.get("path"))
        if name in seen or any(name.startswith(old + "/") or old.startswith(name + "/") for old in seen):
            raise ContractError("duplicate or conflicting input path")
        seen.add(name)
        total += len(decode_file(item))
        if total > MAX_INPUT_BYTES:
            raise ContractError("input snapshot exceeds byte limit")
    supplied = data.get("limits", {})
    maximum = dict(ceilings or DEFAULT_LIMITS)
    if not isinstance(supplied, dict) or set(supplied) - set(maximum):
        raise ContractError("unknown execution limit")
    limits = dict(maximum)
    for key, value in supplied.items():
        valid_type = type(value) in (int, float) if key == "cpus" else type(value) is int
        if not valid_type or not MIN_LIMITS[key] <= value <= maximum[key]:
            raise ContractError(f"{key} exceeds permitted execution limits")
        limits[key] = value
    if limits["disk_mb"] >= limits["memory_mb"]:
        raise ContractError("workspace tmpfs must fit below the container memory limit")
    return {**data, "inputs": files, "limits": limits}


def request_digest(data: dict) -> str:
    return hashlib.sha256(json.dumps(data, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def file_record(path: str, content: bytes) -> dict:
    return {"path": path, "size": len(content), "sha256": hashlib.sha256(content).hexdigest(),
            "mime": PUBLIC_EXTENSIONS.get(PurePosixPath(path).suffix.lower(), "application/octet-stream"),
            "content_base64": base64.b64encode(content).decode("ascii")}


def public_output_metadata(snapshot: list[dict]) -> list[dict]:
    return [{key: item[key] for key in ("path", "size", "sha256", "mime")}
            for item in snapshot if item["path"].startswith("output/")]


def maximum_result_json_estimate(limits: dict) -> int:
    """Conservative bound for canonical v1 JSON, without allocating payloads.

    Each file can have base64 padding. Path metadata occurs in both arrays, but
    bytes occur once. ensure_ascii may expand path/log bytes sixfold.
    """
    count = limits["max_output_files"]
    base64_bytes = 4 * ((limits["max_output_bytes"] + 2 * count + 2) // 3)
    metadata = count * 2 * (768 * 6 + 512)
    logs = limits["max_log_bytes"] * 2 * 6
    return base64_bytes + metadata + logs + 64 * 1024
