"""Run-authorized private staging and immutable artifact publication via Go."""
from __future__ import annotations

import asyncio
import base64
import hashlib
from pathlib import PurePosixPath

from .contracts import RuntimeFailure
from .control import operation_id
from .workspace import MAX_WORKSPACE_BYTES, logical_path

MIMES = {".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
         ".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
         ".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
         ".pdf": "application/pdf", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
         ".webp": "image/webp", ".txt": "text/plain", ".md": "text/markdown", ".csv": "text/csv", ".json": "application/json"}


class ResourceClient:
    def __init__(self, control, workspace):
        self.control, self.workspace = control, workspace
        self.version, self.snapshot_id, self.digest = 0, None, None
        self.head_version = 0
        self.lock = asyncio.Lock()
        self.published = []

    def files(self):
        return [{"path": path, "size": len(content), "sha256": hashlib.sha256(content).hexdigest(),
                 "mime": MIMES.get(PurePosixPath(path).suffix.lower(), "application/octet-stream"),
                 "content_base64": base64.b64encode(content).decode()}
                for path, content in sorted(self.workspace.files.items()) if path.startswith(("uploads/", "output/", "scratch/"))]

    async def restore(self, *, version: int | None = None):
        if version is not None and (type(version) is not int or version < 0):
            raise RuntimeFailure("INVALID_WORKSPACE_VERSION", "任务文件版本无效")
        path = self.control.run_path("workspace") + (f"?version={version}" if version is not None else "")
        data = await self.control.get(path, maximum=40 * 1024 * 1024)
        try:
            files = data["files"] or []
            if len(files) > 64 or type(data["version"]) is not int or data["version"] < 0:
                raise ValueError("invalid snapshot")
            if version is not None and data["version"] != version:
                raise ValueError("snapshot version mismatch")
            head_version = data.get("head_version", data["version"])
            if type(head_version) is not int or head_version < data["version"]:
                raise ValueError("invalid snapshot head")
            restored, total = {}, 0
            for item in files:
                name = logical_path(item["path"])
                if not name.startswith(("uploads/", "output/", "scratch/")) or name in restored:
                    raise ValueError("invalid restore path")
                content = base64.b64decode(item["content_base64"], validate=True)
                total += len(content)
                if total > MAX_WORKSPACE_BYTES or len(content) != item["size"] or hashlib.sha256(content).hexdigest() != item["sha256"]:
                    raise ValueError("invalid restore bytes")
                restored[name] = content
            self.workspace.files = {path: content for path, content in self.workspace.files.items() if path.startswith("skills/")} | restored
            self.version, self.snapshot_id = data["version"], data.get("snapshot_id")
            self.head_version = head_version
            self.digest = self._digest()
        except (ValueError, TypeError, KeyError):
            raise RuntimeFailure("INVALID_WORKSPACE_RESTORE", "任务文件恢复校验失败，未发起新操作", uncertain=True) from None

    def _digest(self):
        return operation_id(str(self.control.manifest.run_id), "workspace", "snapshot", [
            [path, len(data), hashlib.sha256(data).hexdigest()] for path, data in sorted(self.workspace.files.items())
            if path.startswith(("uploads/", "output/", "scratch/"))])

    async def persist(self):
        async with self.lock:
            digest = self._digest()
            if digest == self.digest:
                return self.version
            files = self.files()
            if len(files) > 64:
                raise RuntimeFailure("WORKSPACE_FILE_LIMIT", "任务文件超过云端快照上限")
            save_id = operation_id(str(self.control.manifest.run_id), "workspace", str(self.head_version), digest)
            result = await self.control.post(self.control.run_path("workspace"),
                                             {"operation_id": save_id, "expected_version": self.head_version, "files": files},
                                             operation=save_id, uncertain_on_transport=True)
            if not isinstance(result, dict) or type(result.get("version")) is not int or result["version"] <= self.version:
                raise RuntimeFailure("WORKSPACE_SAVE_UNKNOWN", "任务文件保存未确认，停止后续操作", uncertain=True)
            self.version, self.snapshot_id, self.digest = result["version"], result.get("snapshot_id"), digest
            self.head_version = max(self.head_version, self.version)
            return self.version

    async def publish(self):
        parent = self.control.manifest.output_parent
        if parent is not None and parent.path not in self.workspace.files:
            raise RuntimeFailure("PRIMARY_ARTIFACT_MISSING", "未找到继续修改后的主成果文件，未宣称已交付新版本")
        await self.persist()
        artifacts = []
        files = self.files()
        output_stems = [PurePosixPath(item["path"]).stem for item in files
                        if item["path"].startswith("output/") and not item["path"].startswith("output/previews/")]
        for file in files:
            if not file["path"].startswith("output/") or file["path"].startswith("output/previews/"):
                continue
            suffix = PurePosixPath(file["path"]).suffix.lower()
            if suffix not in MIMES:
                raise RuntimeFailure("UNSUPPORTED_ARTIFACT", "成果类型不允许发布")
            payload = {key: file[key] for key in ("path", "sha256", "mime", "size")}
            payload.update(workspace_version=self.version,
                           publication_id=operation_id(str(self.control.manifest.run_id), "artifact", file["path"], file["sha256"]))
            parent = self.control.manifest.output_parent
            if parent is not None and file["path"] == parent.path:
                payload.update(resource_id=parent.resource_id, parent_version_id=parent.version_id)
            filename, stem = file["path"].removeprefix("output/"), PurePosixPath(file["path"]).stem
            candidates = ["scratch/previews/" + filename + extension for extension in (".pdf", ".png", ".jpg")]
            if suffix in {".docx", ".pptx", ".xlsx"}:
                # Exact selected Bisheng renderer output convention, never a
                # workspace-wide search that could attach another file's page.
                root = "scratch/preview/" + filename + "/"
                candidates += [root + stem + ".pdf", root + ("slide-1.png" if suffix == ".pptx" else "page-01.png")]
                # Previously persisted tasks used a stem-only renderer folder.
                # It is safe only when that stem identifies one output file.
                if output_stems.count(stem) == 1:
                    legacy = "scratch/preview/" + stem + "/"
                    candidates += [legacy + stem + ".pdf", legacy + ("slide-1.png" if suffix == ".pptx" else "page-01.png")]
                    candidates += ["output/previews/" + stem + ".pdf"]
            for preview_path in candidates:
                if preview_path in self.workspace.files:
                    payload["preview_path"] = preview_path
                    break
            result = await self.control.post(self.control.run_path("artifacts"), payload,
                                             operation=payload["publication_id"], uncertain_on_transport=True)
            artifacts.append(result)
            resource_id = result.get("resource_id", result.get("id"))
            if not resource_id or not result.get("version_id"):
                raise RuntimeFailure("ARTIFACT_PUBLICATION_UNKNOWN", "成果归档未返回有效标识，未宣称交付成功", uncertain=True)
            self.published.append(result)
        return artifacts
