"""Bounded file handling shared by the structured Office operations."""

from collections.abc import Iterator
from contextlib import contextmanager
import base64
import hashlib
from pathlib import Path, PurePosixPath
import re
import stat
import unicodedata
import zipfile

MAX_FILE = 24 * 1024 * 1024
MAX_EXPANDED = 128 * 1024 * 1024
MAX_ZIP_FILES = 64
MAX_OFFICE_PARTS = 10_000
MAX_TEXT = 32_000
SHA256 = re.compile(r"^[a-f0-9]{64}$")
OFFICE_PART = {
    ".docx": "word/document.xml",
    ".xlsx": "xl/workbook.xml",
    ".pptx": "ppt/presentation.xml",
}


class OfficeError(Exception):
    def __init__(self, message: str, code: str = "INVALID_DOCUMENT"):
        super().__init__(message)
        self.code = code


def file_name(name: object, extension: str) -> str:
    if not isinstance(name, str) or not name or name in {".", ".."}:
        raise OfficeError("文件名无效。")
    if "/" in name or "\\" in name or len(name.encode("utf-8")) > 240:
        raise OfficeError("文件名无效。")
    if any(unicodedata.category(ch) == "Cc" for ch in name):
        raise OfficeError("文件名无效。")
    if Path(name).suffix.lower() != extension:
        raise OfficeError(f"文件必须使用 {extension} 扩展名。")
    return name


def plain_text(value: object, limit: int = MAX_TEXT, *, empty: bool = True) -> str:
    if not isinstance(value, str) or len(value) > limit or (not empty and not value):
        raise OfficeError("文本参数无效或过长。", "INVALID_ARGUMENT")
    if any(
        (ord(ch) < 32 and ch not in "\t\n\r")
        or 0xD800 <= ord(ch) <= 0xDFFF
        or ord(ch) in (0xFFFE, 0xFFFF)
        for ch in value
    ):
        raise OfficeError("文本包含无效字符。", "INVALID_ARGUMENT")
    return value


def object_list(value: object, maximum: int, *, allow_empty: bool = False) -> list[dict]:
    if not isinstance(value, list) or len(value) > maximum or (not allow_empty and not value):
        raise OfficeError("操作列表为空或超过处理上限。", "INVALID_ARGUMENT")
    if any(not isinstance(item, dict) for item in value):
        raise OfficeError("操作列表格式无效。", "INVALID_ARGUMENT")
    return value


def integer(value: object, minimum: int, maximum: int) -> int:
    if type(value) is not int or not minimum <= value <= maximum:
        raise OfficeError("数字参数超出有效范围。", "INVALID_ARGUMENT")
    return value


def verify_member_name(item: zipfile.ZipInfo) -> str:
    name = item.filename.rstrip("/")
    path = PurePosixPath(name)
    if (
        not name or path.is_absolute() or path.as_posix() != name
        or "\\" in name or ":" in path.parts[0] or ".." in path.parts
        or any(unicodedata.category(ch) == "Cc" for ch in name)
        or len(name.encode("utf-8")) > 1024 or len(path.parts) > 24
    ):
        raise OfficeError("压缩包包含无效路径。")
    mode = stat.S_IFMT(item.external_attr >> 16)
    if item.flag_bits & 1 or mode not in (0, stat.S_IFDIR, stat.S_IFREG):
        raise OfficeError("压缩包包含加密或特殊文件。")
    return name


@contextmanager
def checked_zip(source: Path, extension: str) -> Iterator[zipfile.ZipFile]:
    try:
        with zipfile.ZipFile(source) as archive:
            entries = archive.infolist()
            if len(entries) > (MAX_ZIP_FILES if extension == ".zip" else MAX_OFFICE_PARTS):
                raise OfficeError("压缩包文件数量超过处理上限。", "FILE_TOO_LARGE")
            total = 0
            names = set()
            for entry in entries:
                name = verify_member_name(entry)
                normalized = unicodedata.normalize("NFC", name).casefold()
                if normalized in names:
                    raise OfficeError("压缩包包含重名文件。")
                names.add(normalized)
                total += entry.file_size
                if total > MAX_EXPANDED:
                    raise OfficeError("压缩包解压后超过处理上限。", "FILE_TOO_LARGE")
                if extension != ".zip" and (
                    "vbaproject" in normalized or "/activex/" in normalized
                ):
                    raise OfficeError("此 Office 文件包含宏或活动内容。")
            if extension in OFFICE_PART and (
                OFFICE_PART[extension] not in archive.namelist() or "[Content_Types].xml" not in archive.namelist()
            ):
                raise OfficeError("Office 文件结构无效。")
            yield archive
    except (zipfile.BadZipFile, NotImplementedError) as exc:
        raise OfficeError("无法读取压缩包。") from exc


def file_description(path: Path, name: str) -> dict:
    size = path.stat().st_size
    if size < 1 or size > MAX_FILE:
        raise OfficeError("成品超过文件存储上限。", "FILE_TOO_LARGE")
    digest = hashlib.sha256()
    storage_checksum = hashlib.md5(usedforsecurity=False)
    with path.open("rb") as handle:
        while chunk := handle.read(128 * 1024):
            digest.update(chunk)
            storage_checksum.update(chunk)
    return {
        "name": name, "bytes": size, "sha256": digest.hexdigest(),
        "content_md5": base64.b64encode(storage_checksum.digest()).decode("ascii"),
    }
