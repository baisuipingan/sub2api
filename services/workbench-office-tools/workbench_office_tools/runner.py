"""Subprocess entry point for a single trusted Office operation."""

import json
import os
from pathlib import Path
import resource
import sys

from .files import MAX_FILE, OfficeError
from .operations import perform


def run() -> None:
    directory = Path.cwd()
    request = json.loads((directory / "request.json").read_text())
    output = directory / "output"
    try:
        images = {version: directory / name for version, name in request.get("images", {}).items()}
        status, result, name = perform(
            request["action"], request["args"],
            Path(request["source"]) if request["source"] else None,
            request["input_name"], output, directory, images,
        )
        receipt = {"status": status, "result": result, "name": name}
        if status == "failed":
            receipt["error"] = result.get("error", "OFFICE_FAILED")
            receipt["message"] = result.get("message", "文件处理没有完成。")
    except OfficeError as exc:
        receipt = {"status": "failed", "error": exc.code, "message": str(exc)}
    except (ValueError, TypeError, KeyError, OverflowError):
        receipt = {"status": "failed", "error": "INVALID_ARGUMENT", "message": "文件或工具参数无法处理。"}
    except (MemoryError, OSError):
        receipt = {"status": "failed", "error": "FILE_TOO_LARGE", "message": "文件处理超过内存或磁盘限制。"}
    encoded = json.dumps(receipt, ensure_ascii=False, allow_nan=False)
    if len(encoded.encode()) > 384 * 1024:
        encoded = '{"status":"failed","error":"TOOL_RESULT_TOO_LARGE","message":"工具结果过大，请缩小读取范围。"}'
    (directory / "receipt.json").write_text(encoded, encoding="utf-8")


if __name__ == "__main__":
    # Use a separate hard resource ceiling for each parser process.
    if sys.platform.startswith("linux"):
        resource.setrlimit(resource.RLIMIT_AS, (768 << 20, 768 << 20))
        resource.setrlimit(resource.RLIMIT_CPU, (95, 100))
    resource.setrlimit(resource.RLIMIT_FSIZE, (128 << 20, 128 << 20))
    os.umask(0o077)
    run()
