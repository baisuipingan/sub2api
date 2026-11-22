"""Read and change selected DOCX text without rewriting untouched parts."""

from collections import defaultdict
from pathlib import Path
import re
import zipfile

from lxml import etree
from docx import Document

from .files import OfficeError, checked_zip, file_name, integer, object_list, plain_text

WORD = {
    "http://schemas.openxmlformats.org/wordprocessingml/2006/main",
    "http://purl.oclc.org/ooxml/wordprocessingml/main",
}
DC = "http://purl.org/dc/elements/1.1/"
SPACE = "{http://www.w3.org/XML/1998/namespace}space"
SCOPES = {"all", "title", "body", "header", "footer"}
PAGE_CHARS = 12_000
WORD_PART = re.compile(r"^word/(document|header[^/]*|footer[^/]*)\.xml$")


def document_part(name: str) -> str | None:
    if name == "docProps/core.xml":
        return "title"
    match = WORD_PART.match(name)
    if match:
        part = match[1]
        if part.startswith("header"):
            return "header"
        if part.startswith("footer"):
            return "footer"
        return "body"
    return None


def xml_part(data: bytes) -> etree._Element:
    try:
        parser = etree.XMLParser(resolve_entities=False, no_network=True, recover=False)
        root = etree.fromstring(data, parser=parser)
        if root.getroottree().docinfo.doctype:
            raise OfficeError("Office 文档 XML 不支持外部声明。")
        return root
    except etree.XMLSyntaxError as exc:
        raise OfficeError("Office 文档 XML 无法解析。") from exc


def paragraphs(scope: str, root: etree._Element):
    if scope == "title":
        for title in root.iter(f"{{{DC}}}title"):
            yield [title], True, False
        return
    uri = etree.QName(root).namespace
    if uri not in WORD:
        return
    ns = f"{{{uri}}}"
    for paragraph in root.iter(ns + "p"):
        title = paragraph.find(f"./{ns}pPr/{ns}pStyle")
        is_title = title is not None and title.get(ns + "val", "").lower() in {"title", "subtitle"}
        groups = [[]]
        for node in paragraph.iter():
            if node.tag == ns + "t":
                parents = node.iterancestors()
                parent = next(parents, None)
                if parent is None or parent.tag != ns + "r":
                    continue
                blocked = False
                for ancestor in parents:
                    if ancestor is paragraph:
                        break
                    if ancestor.tag in {ns + "p", ns + "del", ns + "moveFrom"}:
                        blocked = True
                        break
                if not blocked:
                    groups[-1].append(node)
            elif node.tag in {ns + "tab", ns + "br", ns + "cr", ns + "drawing", ns + "pict", ns + "fldChar", ns + "instrText"}:
                if groups[-1]:
                    groups.append([])
        for index, group in enumerate(groups):
            if group:
                yield group, is_title, index != 0


def validate_edits(args: dict) -> list[tuple[str, str, str]]:
    edits = []
    for value in object_list(args.get("edits"), 100):
        old = plain_text(value.get("old_text"), 4000, empty=False)
        new = plain_text(value.get("new_text"), 8000)
        scope = value.get("scope") or "all"
        if not isinstance(scope, str) or scope not in SCOPES or any(ch in old + new for ch in "\t\n\r"):
            raise OfficeError("Word 替换要求单行文字和有效的范围。", "INVALID_ARGUMENT")
        if new == old:
            raise OfficeError("替换前后的文字相同。", "INVALID_ARGUMENT")
        edits.append((old, new, scope))
    return edits


def scope_matches(requested: str, section: str, title: bool) -> bool:
    return requested in ("all", section) or (requested == "title" and title)


def replace_nodes(nodes: list, old: str, new: str, allowance: list[int]) -> int:
    existing = "".join(node.text or "" for node in nodes)
    count = existing.count(old)
    if count == 0:
        return 0
    allowance[0] -= count * max(0, len(new.encode()) - len(old.encode()))
    if allowance[0] < 0:
        raise OfficeError("替换后的文字超过处理上限。", "FILE_TOO_LARGE")
    # Split original source into ranges without indexing by UTF-8 bytes.
    # Preserve line/runs outside matches and keep inserted text in its first run.
    positions = []
    position = 0
    for node in nodes:
        end = position + len(node.text or "")
        positions.append((position, end, node))
        position = end
    updated = defaultdict(list)
    index = 0

    def keep(start: int, end: int):
        nonlocal index
        while index < len(positions):
            left, right, node = positions[index]
            if left >= end:
                break
            if right > start:
                updated[id(node)].append(existing[max(start, left):min(end, right)])
            if right > end:
                break
            index += 1

    cursor = 0
    for match in re.finditer(re.escape(old), existing):
        start, end = match.span()
        keep(cursor, start)
        while index < len(positions) and positions[index][1] <= start:
            index += 1
        if index < len(positions):
            updated[id(positions[index][2])].append(new)
        cursor = end
    keep(cursor, len(existing))
    for node in nodes:
        node.text = "".join(updated[id(node)])
        if node.text and (node.text[0].isspace() or node.text[-1].isspace()):
            node.set(SPACE, "preserve")
    return count


def create_docx(args: dict, output: Path) -> tuple[dict, str]:
    name = file_name(args.get("name") or "文档.docx", ".docx")
    title = plain_text(args.get("title") or "")
    body = args.get("paragraphs", [])
    if not isinstance(body, list) or len(body) > 1000 or not (title or body):
        raise OfficeError("请提供 Word 标题或段落。", "INVALID_ARGUMENT")
    doc = Document()
    if title:
        doc.core_properties.title = title
        doc.add_paragraph(title, style="Title")
    for paragraph in body:
        doc.add_paragraph(plain_text(paragraph))
    doc.save(output)
    read_docx(output, {})
    return {"verified": True}, name


def read_docx(source: Path, args: dict) -> dict:
    requested = args.get("scope") or "all"
    if not isinstance(requested, str) or requested not in SCOPES:
        raise OfficeError("Word 读取范围无效。", "INVALID_ARGUMENT")
    offset = requested_offset = integer(args.get("offset", 0), 0, 128_000_000)
    remaining = PAGE_CHARS
    sections = []
    used = 0
    with checked_zip(source, ".docx") as archive:
        for name in archive.namelist():
            scope = document_part(name)
            if scope is None or (requested != "all" and scope not in {requested, "body"}):
                continue
            root = xml_part(archive.read(name))
            chunks = []
            for nodes, title, continuance in paragraphs(scope, root):
                if not scope_matches(requested, scope, title):
                    continue
                text = "".join(node.text or "" for node in nodes)
                if not text:
                    continue
                if continuance:
                    text = "\t" + text
                else:
                    text += "\n"
                if offset >= len(text):
                    offset -= len(text)
                    continue
                text = text[offset:]
                offset = 0
                if not remaining:
                    if chunks:
                        sections.append({"scope": scope, "part": name, "text": "".join(chunks)})
                    return {"sections": sections, "next_offset": requested_offset + used}
                chunk = text[:remaining]
                chunks.append(chunk)
                remaining -= len(chunk)
                used += len(chunk)
                if len(chunk) < len(text):
                    sections.append({"scope": scope, "part": name, "text": "".join(chunks)})
                    return {"sections": sections, "next_offset": requested_offset + used}
            if chunks:
                sections.append({"scope": scope, "part": name, "text": "".join(chunks)})
    return {"sections": sections}


def edit_docx(source: Path, output: Path, edits: list[tuple[str, str, str]]) -> dict:
    counts = [0] * len(edits)
    changed = {}
    allowance = [16 * 1024 * 1024]
    with checked_zip(source, ".docx") as archive:
        for name in archive.namelist():
            scope = document_part(name)
            if scope is None:
                continue
            root = xml_part(archive.read(name))
            groups = list(paragraphs(scope, root))
            has_match = False
            for index, (old, new, requested) in enumerate(edits):
                for nodes, title, _ in groups:
                    if scope_matches(requested, scope, title):
                        count = replace_nodes(nodes, old, new, allowance)
                        counts[index] += count
                        has_match |= count > 0
            if has_match:
                changed[name] = etree.tostring(root, encoding="utf-8", xml_declaration=True)
        if not changed:
            raise OfficeError("文档中没有找到待替换的文字。", "NO_MATCH")
        with zipfile.ZipFile(output, "w") as result:
            for part in archive.infolist():
                result.writestr(part, changed.get(part.filename) or archive.read(part), compress_type=zipfile.ZIP_DEFLATED)
    with checked_zip(output, ".docx") as archive:
        for name, expected in changed.items():
            actual = archive.read(name)
            xml_part(actual)
            if actual != expected:
                raise OfficeError("保存后无法核对文档修改。", "VERIFY_FAILED")
    return {"verified": True, "replacements": counts}
