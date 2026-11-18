"""Bounded RAM-only virtual filesystem for phase zero; no host paths or exec.

Not a durable resource store. This deliberately does not put binary attachments
or signed URLs into LangGraph checkpoints. Production resource restore is gated.
"""
from __future__ import annotations

import base64
import fnmatch
import hashlib
from pathlib import Path

from deepagents.backends.protocol import (BackendProtocol, EditResult, FileDownloadResponse,
    FileUploadResponse, GlobResult, GrepResult, LsResult, ReadResult, WriteResult)

MAX_WORKSPACE_BYTES = 24 * 1024 * 1024
MAX_FILES = 128
MAX_TEXT_BYTES = 256 * 1024
MAX_SKILL_FILES = 64
MAX_SKILL_BYTES = 2 * 1024 * 1024


def vendor_root() -> Path:
    packaged = Path(__file__).parent / "vendor" / "bisheng"
    return packaged if packaged.is_dir() else Path(__file__).parents[2] / "vendor" / "bisheng"


def logical_path(path: str, *, allow_root=False) -> str:
    if (not isinstance(path, str) or "\\" in path or ":" in path or
            any(ord(c) < 32 for c in path) or len(path.encode()) > 768):
        raise ValueError("invalid virtual path")
    value = path.removeprefix("/")
    if value == "" and allow_root:
        return ""
    if not value or any(part in {"", ".", ".."} for part in value.rstrip("/").split("/")):
        raise ValueError("invalid virtual path")
    value = value.rstrip("/")
    if len(value.split("/")) > 12:
        raise ValueError("virtual path is too deep")
    if value.split("/", 1)[0] not in {"uploads", "output", "scratch", "skills", "large_tool_results"}:
        raise ValueError("path is outside the task workspace")
    return value


class VirtualWorkspace(BackendProtocol):
    def __init__(self, *, with_skills: bool = True):
        self.files: dict[str, bytes] = {}
        if with_skills:
            # This is a fixed, versioned application bundle, not a caller path.
            root = vendor_root() / "skills"
            for path in root.rglob("*"):
                if path.is_file() and not path.is_symlink():
                    self.files["skills/" + path.relative_to(root).as_posix()] = path.read_bytes()
            if len(self.files) > MAX_SKILL_FILES or sum(map(len, self.files.values())) > MAX_SKILL_BYTES:
                raise ValueError("fixed skill bundle exceeds its audited size ceiling")

    def _info(self, path):
        return {"path": "/" + path, "size": len(self.files[path]), "is_dir": False}

    def _set(self, path: str, content: bytes):
        combined = dict(self.files)
        combined[path] = content
        if not self._task_budget_ok(combined):
            raise ValueError("workspace limit exceeded")
        self.files[path] = content

    @staticmethod
    def _task_budget_ok(files):
        # Fixed, audited application skills are not user task files. Counting
        # them against executor's 128-file/24-MiB contract creates false rejects.
        task_files = [data for name, data in files.items() if not name.startswith("skills/")]
        return len(task_files) <= MAX_FILES and sum(map(len, task_files)) <= MAX_WORKSPACE_BYTES

    def ls(self, path="/"):
        try:
            prefix = logical_path(path, allow_root=True)
            prefix = prefix + "/" if prefix else ""
            entries = {}
            for name in sorted(self.files):
                if name.startswith(prefix):
                    first, sep, _ = name[len(prefix):].partition("/")
                    key = prefix + first
                    entries[key] = {"path": "/" + key, "is_dir": True} if sep else self._info(name)
            return LsResult(entries=list(entries.values()))
        except ValueError as exc:
            return LsResult(error=str(exc))

    def read(self, file_path, offset=0, limit=2000):
        try:
            name = logical_path(file_path)
            data = self.files[name]
            if Path(name).suffix.lower() in {".docx", ".xlsx", ".pptx", ".pdf", ".png", ".jpg", ".jpeg", ".webp", ".gif", ".zip"}:
                raise ValueError("binary file; inspect it using the office executor")
            if len(data) > MAX_TEXT_BYTES:
                raise ValueError("file is too large for direct text read; inspect it using the office executor")
            text = data.decode("utf-8")
            if "\x00" in text:
                raise ValueError("binary file; inspect it using the office executor")
            if offset < 0 or limit < 1 or limit > 2000:
                raise ValueError("invalid read range")
            # Backend returns raw text; FilesystemMiddleware owns line formatting.
            return ReadResult(file_data={"content": "\n".join(text.splitlines()[offset:offset + limit]), "encoding": "utf-8"})
        except (ValueError, UnicodeDecodeError, KeyError) as exc:
            return ReadResult(error="file missing, binary, or outside read budget" if isinstance(exc, (KeyError, UnicodeDecodeError)) else str(exc))

    def write(self, file_path, content):
        try:
            name = logical_path(file_path)
            if not name.startswith(("scratch/", "output/", "large_tool_results/")):
                raise ValueError("uploads and skills are read-only")
            if name in self.files:
                raise ValueError("file exists; use edit_file")
            data = content.encode("utf-8")
            if len(data) > MAX_TEXT_BYTES:
                raise ValueError("text write exceeds budget")
            self._set(name, data)
            return WriteResult(path="/" + name)
        except ValueError as exc:
            return WriteResult(error=str(exc))

    def edit(self, file_path, old_string, new_string, replace_all=False):
        try:
            name = logical_path(file_path)
            if not name.startswith(("scratch/", "output/", "large_tool_results/")):
                raise ValueError("uploads and skills are read-only")
            data = self.files[name].decode("utf-8")
            count = data.count(old_string) if old_string else 0
            if not count or (not replace_all and count != 1):
                raise ValueError("old_string is absent or ambiguous")
            updated = data.replace(old_string, new_string, -1 if replace_all else 1).encode()
            if len(updated) > MAX_TEXT_BYTES:
                raise ValueError("text write exceeds budget")
            self._set(name, updated)
            return EditResult(path="/" + name, occurrences=count if replace_all else 1)
        except (ValueError, KeyError, UnicodeDecodeError):
            return EditResult(error="cannot edit this path or replacement")

    def glob(self, pattern, path=None):
        try:
            prefix = logical_path(path or "/", allow_root=True)
            return GlobResult(matches=[self._info(n) for n in sorted(self.files)
                if (not prefix or n.startswith(prefix + "/")) and fnmatch.fnmatch("/" + n, pattern if pattern.startswith("/") else "*/" + pattern)])
        except ValueError as exc:
            return GlobResult(error=str(exc))

    def grep(self, pattern, path=None, glob=None):
        try:
            prefix = logical_path(path or "/", allow_root=True)
            matches = []
            for name, content in sorted(self.files.items()):
                if prefix and name != prefix and not name.startswith(prefix + "/"):
                    continue
                if glob and not fnmatch.fnmatch(name, glob):
                    continue
                if len(content) > MAX_TEXT_BYTES:
                    continue
                try:
                    text = content.decode()
                except UnicodeDecodeError:
                    continue
                for i, line in enumerate(text.splitlines(), 1):
                    if pattern in line:
                        matches.append({"path": "/" + name, "line": i, "text": line[:2000]})
                        if len(matches) == 100:
                            return GrepResult(matches=matches)
            return GrepResult(matches=matches)
        except ValueError as exc:
            return GrepResult(error=str(exc))

    def upload_files(self, files):
        raise NotImplementedError("uploads require a trusted resource adapter")

    def download_files(self, paths):
        rows = []
        for path in paths:
            try:
                name = logical_path(path)
                rows.append(FileDownloadResponse(path=path, content=self.files.get(name),
                            error=None if name in self.files else "file_not_found"))
            except ValueError:
                rows.append(FileDownloadResponse(path=path, error="permission_denied"))
        return rows

    def executor_inputs(self):
        return [{"path": name, "content_base64": base64.b64encode(data).decode(), "sha256": hashlib.sha256(data).hexdigest()}
                for name, data in sorted(self.files.items()) if name.startswith(("uploads/", "scratch/", "output/"))]

    def replace_executor_snapshot(self, rows):
        snapshot = {}
        for row in rows:
            name = logical_path(row["path"])
            if not name.startswith(("output/", "scratch/")) or name in snapshot:
                raise ValueError("executor snapshot contains an invalid or duplicate path")
            data = base64.b64decode(row["content_base64"], validate=True)
            if len(data) != row["size"] or hashlib.sha256(data).hexdigest() != row["sha256"]:
                raise ValueError("executor snapshot integrity check failed")
            snapshot[name] = data
        combined = {n: d for n, d in self.files.items() if not n.startswith(("output/", "scratch/"))} | snapshot
        if not self._task_budget_ok(combined):
            raise ValueError("executor snapshot exceeds workspace limits")
        self.files = combined
