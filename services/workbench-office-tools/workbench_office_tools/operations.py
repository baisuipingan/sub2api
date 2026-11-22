"""Structured actions available to the workbench tool service."""

from contextlib import contextmanager
from copy import copy
import base64
from hashlib import sha256
import io
import json
import math
from pathlib import Path, PurePosixPath
import re
import shutil
import warnings
import zipfile

from openpyxl import Workbook, load_workbook
from openpyxl.cell.cell import MergedCell
from openpyxl.styles.numbers import is_timedelta_format
from openpyxl.utils.cell import coordinate_to_tuple, range_boundaries
from openpyxl.utils.datetime import from_excel
from openpyxl.utils.exceptions import InvalidFileException
from PIL import Image
from pptx import Presentation
from pptx.oxml.ns import qn
from pptx.shapes.picture import Picture
from pptx.util import Inches

from .files import MAX_FILE, MAX_ZIP_FILES, OfficeError, checked_zip, file_name, integer, object_list, plain_text
from .word import create_docx, edit_docx, read_docx, validate_edits

ACTIONS = {
    "create_docx": (None, ".docx"), "read_docx": (".docx", None), "edit_docx": (".docx", ".docx"),
    "batch_edit_docx": (".zip", ".zip"),
    "render_docx_pdf": (".docx", ".pdf"),
    "recalc_xlsx": (".xlsx", ".xlsx"),
    "create_xlsx": (None, ".xlsx"), "read_xlsx": (".xlsx", None), "edit_xlsx": (".xlsx", ".xlsx"),
    "create_pptx": (None, ".pptx"), "template_pptx": (".pptx", ".pptx"),
    "read_pptx": (".pptx", None), "edit_pptx": (".pptx", ".pptx"),
}
CELL = re.compile(r"^[A-Z]{1,3}[1-9][0-9]{0,6}$")
INVALID_SHEET = re.compile(r"[\\*?:/\[\]]")
MAX_CELLS = 100_000
MAX_READ_CELLS = 2000


def checked_cell(value: object) -> str:
    if not isinstance(value, str) or not CELL.fullmatch(value):
        raise OfficeError("单元格地址无效。", "INVALID_ARGUMENT")
    row, column = coordinate_to_tuple(value)
    if row > 1_048_576 or column > 16_384:
        raise OfficeError("单元格超出 Excel 范围。", "INVALID_ARGUMENT")
    return value


def checked_range(span: object) -> tuple[int, int, int, int]:
    if not isinstance(span, str) or len(span) > 32:
        raise OfficeError("单元格范围无效。", "INVALID_ARGUMENT")
    try:
        col1, row1, col2, row2 = range_boundaries(span)
    except ValueError as exc:
        raise OfficeError("单元格范围无效。", "INVALID_ARGUMENT") from exc
    if any(value is None for value in (col1, row1, col2, row2)):
        raise OfficeError("请输入明确的范围，例如 A1:D20。", "INVALID_ARGUMENT")
    if not 1 <= col1 <= col2 <= 16_384 or not 1 <= row1 <= row2 <= 1_048_576:
        raise OfficeError("单元格范围超出 Excel 边界或起止顺序无效。", "INVALID_ARGUMENT")
    return col1, row1, col2, row2


def cell_value(value: object):
    if isinstance(value, str):
        return plain_text(value, 32_767)
    if value is None or isinstance(value, bool):
        return value
    if isinstance(value, (int, float)) and abs(value) <= 1e308 and math.isfinite(value):
        return value
    raise OfficeError("单元格数据类型无效。", "INVALID_ARGUMENT")


def set_cell(cell, change: dict) -> None:
    if isinstance(cell, MergedCell):
        raise OfficeError("合并区域只支持修改左上角的单元格。", "INVALID_ARGUMENT")
    if "value" in change:
        cell.value = cell_value(change["value"])
    if "format" in change:
        cell.number_format = plain_text(change["format"], 255, empty=False)
    if "bold" in change:
        if type(change["bold"]) is not bool:
            raise OfficeError("bold 需要是布尔值。", "INVALID_ARGUMENT")
        font = copy(cell.font)
        font.bold = change["bold"]
        cell.font = font


@contextmanager
def open_xlsx(source: Path, **options):
    with source.open("rb") as handle:
        try:
            workbook = load_workbook(handle, **options)
        except InvalidFileException as exc:
            raise OfficeError("Excel 工作簿结构无效。") from exc
        try:
            yield workbook
        finally:
            workbook.close()


def verify_xlsx(output: Path, expected: dict) -> None:
    with checked_zip(output, ".xlsx"):
        pass
    with open_xlsx(output, read_only=not expected, keep_links=False, rich_text=True) as reloaded:
        for (sheet, coord), (value, num_format, bold) in expected.items():
            cell = reloaded[sheet][coord]
            actual_bold = bool(cell.font and cell.font.b)
            if (cell.value, cell.number_format, actual_bold) != (value, num_format, bold):
                raise OfficeError("保存后无法核对单元格修改。", "VERIFY_FAILED")


def create_xlsx(args: dict, output: Path) -> tuple[dict, str]:
    name = file_name(args.get("name") or "工作簿.xlsx", ".xlsx")
    workbook = Workbook()
    workbook.remove(workbook.active)
    cells = 0
    sheets = object_list(args.get("sheets"), 32)
    for sheet in sheets:
        title = plain_text(sheet.get("name"), 31, empty=False)
        if INVALID_SHEET.search(title) or title.lower() in {value.lower() for value in workbook.sheetnames}:
            raise OfficeError("工作表名称无效或重复。", "INVALID_ARGUMENT")
        worksheet = workbook.create_sheet(title)
        rows = sheet.get("rows", [])
        if not isinstance(rows, list):
            raise OfficeError("工作表行必须为数组。", "INVALID_ARGUMENT")
        for row in rows:
            if not isinstance(row, list) or len(row) > 512:
                raise OfficeError("工作表行格式无效。", "INVALID_ARGUMENT")
            cells += len(row)
            if cells > MAX_CELLS:
                raise OfficeError("单元格数量超过处理上限。", "FILE_TOO_LARGE")
            worksheet.append([cell_value(value) for value in row])
        for style in object_list(sheet.get("styles", []), 512, allow_empty=True):
            set_cell(worksheet[checked_cell(style.get("cell"))], style)
    workbook.save(output)
    verify_xlsx(output, {})
    return {"verified": True, "sheets": workbook.sheetnames}, name


def read_xlsx(source: Path, args: dict) -> dict:
    with checked_zip(source, ".xlsx"):
        pass
    with open_xlsx(source, read_only=True, keep_links=False, data_only=args.get("data_only") is True) as workbook:
        sheet = args.get("sheet") or workbook.sheetnames[0]
        if not isinstance(sheet, str) or sheet not in workbook.sheetnames:
            raise OfficeError("工作表不存在。", "INVALID_ARGUMENT")
        worksheet = workbook[sheet]
        span = args.get("range")
        if span:
            col1, row1, col2, row2 = checked_range(span)
        else:
            col1, row1 = 1, 1
            col2 = min(worksheet.max_column or 20, 20)
            row2 = min(worksheet.max_row or 30, 30)
        if (col2 - col1 + 1) * (row2 - row1 + 1) > MAX_READ_CELLS:
            raise OfficeError("请缩小读取的单元格范围。", "FILE_TOO_LARGE")
        result = {"sheet": sheet, "sheets": workbook.sheetnames, "start_row": row1, "start_column": col1, "rows": []}
        budget = 40_000
        for row in worksheet.iter_rows(min_row=row1, max_row=row2, min_col=col1, max_col=col2, values_only=True):
            safe = []
            for value in row:
                if value is not None and not isinstance(value, (str, bool, int, float)):
                    value = str(value)
                if isinstance(value, str) and len(value) > 800:
                    value = value[:800] + "…"
                    result["truncated"] = True
                budget -= len(str(value))
                safe.append(value)
            if budget < 0:
                result["truncated"] = True
                break
            result["rows"].append(safe)
        return result


def edit_xlsx(source: Path, output: Path, args: dict) -> dict:
    with checked_zip(source, ".xlsx"):
        pass
    cells = object_list(args.get("cells", []), 1000, allow_empty=True)
    ranges = object_list(args.get("ranges", []), 1000, allow_empty=True)
    if not (cells or ranges):
        raise OfficeError("请提供要修改的单元格或区域。", "INVALID_ARGUMENT")
    with warnings.catch_warnings(record=True) as emitted, open_xlsx(source, keep_links=True, rich_text=True) as workbook:
        if any("not supported" in str(entry.message).lower() for entry in emitted):
            raise OfficeError("工作簿含有暂不能保留的 Excel 扩展。", "UNSUPPORTED_FORMAT")
        expected = {}

        def update(cell, change: dict) -> None:
            set_cell(cell, change)
            value = cell.value
            if isinstance(value, str) and not value:
                value = None
            elif type(value) in (float, int) and cell.is_date:
                value = from_excel(value, workbook.epoch, timedelta=is_timedelta_format(cell.number_format))
            expected[cell.parent.title, cell.coordinate] = (value, cell.number_format, bool(cell.font.b))

        def get_sheet(change: dict):
            sheet = change.get("sheet")
            if not isinstance(sheet, str) or sheet not in workbook.sheetnames:
                raise OfficeError("工作表不存在。", "INVALID_ARGUMENT")
            return workbook[sheet]

        for change in cells:
            update(get_sheet(change)[checked_cell(change.get("cell"))], change)
        changed_area = len(cells)
        for change in ranges:
            worksheet = get_sheet(change)
            col1, row1, col2, row2 = checked_range(change.get("range"))
            width, height = col2 - col1 + 1, row2 - row1 + 1
            changed_area += width * height
            if changed_area > MAX_CELLS:
                raise OfficeError("修改的单元格数量超过处理上限。", "FILE_TOO_LARGE")
            style = {key: change[key] for key in ("format", "bold") if key in change}
            values = change.get("values")
            if "values" in change:
                if not isinstance(values, list) or len(values) != height or any(
                    not isinstance(row, list) or len(row) != width for row in values
                ):
                    raise OfficeError("单元格数据的行列数必须和修改区域一致。", "INVALID_ARGUMENT")
            elif not style:
                raise OfficeError("请提供区域数据或格式。", "INVALID_ARGUMENT")
            for row in range(height):
                for column in range(width):
                    cell = worksheet.cell(row1 + row, col1 + column)
                    update(cell, style if values is None else {**style, "value": values[row][column]})
        workbook.save(output)
    del workbook
    verify_xlsx(output, expected)
    result = {"verified": True, "cells_changed": len(expected), "sheets": sorted({sheet for sheet, _ in expected})}
    if ranges:
        result["ranges_changed"] = len(ranges)
    return result


def verify_slide_image(handle, expected_format: str | None = None) -> None:
    try:
        with Image.open(handle, formats=["PNG", "JPEG"]) as image:
            if expected_format is not None and image.format != expected_format:
                raise OfficeError("替换图片的内容与扩展名不一致。", "INVALID_ARGUMENT")
            if image.width * image.height > 16_000_000:
                raise OfficeError("图片像素数超过上限。", "FILE_TOO_LARGE")
            image.verify()
    except (ValueError, OSError, Image.DecompressionBombError) as exc:
        raise OfficeError("仅支持有效的 PNG 或 JPEG 图片。", "INVALID_ARGUMENT") from exc
    handle.seek(0)


def slide_image(value: object) -> io.BytesIO:
    if not isinstance(value, str) or len(value) > 180_000:
        raise OfficeError("图片数据无效或过大。", "INVALID_ARGUMENT")
    try:
        handle = io.BytesIO(base64.b64decode(value, validate=True))
    except ValueError as exc:
        raise OfficeError("图片数据无效。", "INVALID_ARGUMENT") from exc
    verify_slide_image(handle)
    return handle


def presentation_layouts(presentation):
    return [layout for master in presentation.slide_masters for layout in master.slide_layouts]


def add_slide(presentation, spec: dict) -> None:
    layouts = presentation_layouts(presentation)
    layout = integer(spec.get("layout", min(1, len(layouts) - 1)), 0, len(layouts) - 1)
    slide = presentation.slides.add_slide(layouts[layout])
    title = plain_text(spec.get("title") or "")
    content = plain_text(spec.get("text") or "")
    if slide.shapes.title is not None:
        slide.shapes.title.text = title
    elif title:
        slide.shapes.add_textbox(Inches(.7), Inches(.5), Inches(9), Inches(.7)).text = title
    if content:
        placeholder = next((item for item in slide.placeholders if item != slide.shapes.title and item.has_text_frame), None)
        if placeholder is None:
            placeholder = slide.shapes.add_textbox(Inches(.7), Inches(1.5), Inches(9), Inches(4.7))
        placeholder.text = content
    if spec.get("image_base64"):
        slide.shapes.add_picture(slide_image(spec["image_base64"]), Inches(6.6), Inches(4.2), height=Inches(2))


def remove_slides(presentation, indices) -> None:
    for index in sorted(indices, reverse=True):
        slide_id = presentation.slides._sldIdLst[index]
        presentation.part.drop_rel(slide_id.rId)
        presentation.slides._sldIdLst.remove(slide_id)


def create_pptx(args: dict, output: Path, template: Path | None = None) -> tuple[dict, str]:
    name = file_name(args.get("name") or "演示稿.pptx", ".pptx")
    if template is not None:
        with checked_zip(template, ".pptx"):
            pass
    presentation = Presentation(template)
    keep = args.get("keep_template_slides", False)
    if type(keep) is not bool:
        raise OfficeError("是否保留模板原有页面需要设置为布尔值。", "INVALID_ARGUMENT")
    original_count = len(presentation.slides)
    for spec in object_list(args.get("slides"), 100):
        add_slide(presentation, spec)
    if not keep:
        remove_slides(presentation, range(original_count))
    count = len(presentation.slides)
    presentation.save(output)
    verify_pptx(output, count)
    result = {"verified": True, "slides": count}
    if template is not None:
        result["template_slides_kept"] = original_count if keep else 0
    return result, name


def presentation_text(slide):
    for shape in slide.shapes:
        if shape.has_text_frame:
            yield shape.text_frame
        if shape.has_table:
            for row in shape.table.rows:
                for cell in row.cells:
                    yield cell.text_frame


def read_pptx(source: Path, args: dict) -> dict:
    with checked_zip(source, ".pptx"):
        pass
    presentation = Presentation(source)
    offset = integer(args.get("offset", 0), 0, len(presentation.slides))
    budget = 24_000
    slides = []
    for index in range(offset, min(offset + 20, len(presentation.slides))):
        slide = presentation.slides[index]
        title_shape = slide.shapes.title
        title = title_shape.text[:1000] if title_shape is not None else ""
        paragraphs = []
        for frame in presentation_text(slide):
            text = frame.text[:2000]
            if text:
                paragraphs.append(text)
        text = "\n".join(paragraphs)[: min(budget, 12_000)]
        pictures = [
            {"shape": number, "name": shape.name[:160]} for number, shape in enumerate(slide.shapes, 1)
            if isinstance(shape, Picture)
        ][:100]
        budget -= len(title) + len(text) + sum(len(picture["name"]) + 40 for picture in pictures)
        slides.append({"number": index + 1, "title": title, "text": text, "pictures": pictures})
        if budget <= 0:
            break
    result = {
        "slides": slides, "total": len(presentation.slides),
        "layouts": [{"index": index, "name": layout.name[:160]} for index, layout in enumerate(presentation_layouts(presentation))],
    }
    if offset + len(slides) < len(presentation.slides):
        result["next_offset"] = offset + len(slides)
    return result


def verify_pptx(output: Path, count: int, images: dict | None = None) -> None:
    with checked_zip(output, ".pptx"):
        pass
    slides = Presentation(output).slides
    if len(slides) != count:
        raise OfficeError("保存后无法核对幻灯片数量。", "VERIFY_FAILED")
    for (slide_id, index), digest in (images or {}).items():
        slide = slides.get(slide_id)
        if slide is None or sha256(slide.shapes[index].image.blob).hexdigest() != digest:
            raise OfficeError("保存后无法核对替换的图片。", "VERIFY_FAILED")


def replace_slide_image(slide, edit: dict, attachments: dict[str, Path]) -> tuple[int, str]:
    index = integer(edit.get("shape"), 1, len(slide.shapes))
    shape = slide.shapes[index - 1]
    if not isinstance(shape, Picture) or shape._pic.blipFill.blip is None:
        raise OfficeError("指定的图形不是图片。", "INVALID_ARGUMENT")
    if "image_version_id" in edit:
        path = attachments.get(edit["image_version_id"])
        if path is None or "image_base64" in edit:
            raise OfficeError("替换图片版本无效。", "INVALID_ARGUMENT")
        with path.open("rb") as image:
            verify_slide_image(image, "PNG" if path.suffix == ".png" else "JPEG")
            image_part, relation = slide.part.get_or_add_image_part(image)
    else:
        with slide_image(edit.get("image_base64")) as image:
            image_part, relation = slide.part.get_or_add_image_part(image)
    # Retain the existing picture XML, including bounds, crop, rotation and shape ID.
    blip = shape._pic.blipFill.blip
    old_rels = {blip.rEmbed, blip.attrib.pop(qn("r:link"), None)}
    svg_blip = "{http://schemas.microsoft.com/office/drawing/2016/SVG/main}svgBlip"
    for alternate in blip.iter(svg_blip):
        old_rels.add(alternate.get(qn("r:embed")))
        alternate.getparent().remove(alternate)
    blip.rEmbed = relation
    references = set(slide._element.xpath("//@r:*"))
    for old in old_rels:
        if old and old not in references and old in slide.part.rels:
            slide.part.drop_rel(old)
    return index - 1, sha256(image_part.blob).hexdigest()


def edit_pptx(source: Path, output: Path, args: dict, attachments: dict[str, Path]) -> dict:
    with checked_zip(source, ".pptx"):
        pass
    from .word import replace_nodes

    presentation = Presentation(source)
    replacements = []
    specs = object_list(args.get("replacements", []), 100, allow_empty=True)
    added = object_list(args.get("add_slides", []), 100, allow_empty=True)
    images = object_list(args.get("replace_images", []), 32, allow_empty=True)
    remove = args.get("remove_slides", [])
    if not isinstance(remove, list) or len(remove) > 100:
        raise OfficeError("要删除的幻灯片编号无效。", "INVALID_ARGUMENT")
    if not (specs or added or images or remove):
        raise OfficeError("请提供幻灯片修改操作。", "INVALID_ARGUMENT")
    sizes = [16 * 1024 * 1024]
    for spec in specs:
        old = plain_text(spec.get("old_text"), 4000, empty=False)
        new = plain_text(spec.get("new_text"), 8000)
        first = integer(spec.get("slide", 1), 1, len(presentation.slides)) - 1
        last = first + 1 if "slide" in spec else len(presentation.slides)
        count = 0
        for index in range(first, last):
            for frame in presentation_text(presentation.slides[index]):
                for paragraph in frame.paragraphs:
                    nodes = [run._r.find("{http://schemas.openxmlformats.org/drawingml/2006/main}t") for run in paragraph.runs]
                    count += replace_nodes([node for node in nodes if node is not None], old, new, sizes)
        replacements.append(count)
    indices = {integer(value, 1, len(presentation.slides)) - 1 for value in remove}
    expected_images = {}
    for image in images:
        index = integer(image.get("slide"), 1, len(presentation.slides)) - 1
        if index in indices:
            raise OfficeError("无法替换将被删除的幻灯片中的图片。", "INVALID_ARGUMENT")
        slide = presentation.slides[index]
        shape, digest = replace_slide_image(slide, image, attachments)
        expected_images[slide.slide_id, shape] = digest
    for spec in added:
        add_slide(presentation, spec)
    remove_slides(presentation, indices)
    count = len(presentation.slides)
    if count == 0:
        raise OfficeError("幻灯片至少保留一页。", "INVALID_ARGUMENT")
    presentation.save(output)
    verify_pptx(output, count, expected_images)
    result = {"verified": True, "replacements": replacements, "slides": count, "images_replaced": len(images)}
    if specs and not all(replacements):
        result["unmatched"] = sum(count == 0 for count in replacements)
    return result


def batch_edit_docx(source: Path, output: Path, directory: Path, args: dict) -> tuple[str, dict]:
    edits = validate_edits(args)
    files = []
    succeeded = skipped = failed = 0
    with checked_zip(source, ".zip") as archive, zipfile.ZipFile(output, "w", zipfile.ZIP_DEFLATED) as result:
        manifest_name = "workbench-result.json"
        taken = {name.casefold() for name in archive.namelist()}
        while manifest_name.casefold() in taken:
            manifest_name = "_" + manifest_name
        for index, entry in enumerate(archive.infolist()):
            name = entry.filename
            if entry.is_dir():
                result.writestr(name, b"")
                continue
            if PurePosixPath(name).suffix.lower() != ".docx":
                with archive.open(entry) as before, result.open(name, "w") as after:
                    shutil.copyfileobj(before, after)
                skipped += 1
                files.append({"name": name, "status": "skipped"})
                continue
            original, edited = directory / f"source-{index}", directory / f"edited-{index}"
            with archive.open(entry) as before, original.open("wb") as after:
                shutil.copyfileobj(before, after)
            detail = {"name": name}
            try:
                if original.stat().st_size > MAX_FILE:
                    raise OfficeError("单个 Word 文件过大。", "FILE_TOO_LARGE")
                operation = edit_docx(original, edited, edits)
            except OfficeError as exc:
                status = "skipped" if exc.code == "NO_MATCH" else "failed"
                if status == "failed":
                    failed += 1
                else:
                    skipped += 1
                detail.update(status=status, message=str(exc))
                result.write(original, arcname=name)
            else:
                succeeded += 1
                detail.update(status="completed", **operation)
                result.write(edited, arcname=name)
            finally:
                original.unlink(missing_ok=True)
                edited.unlink(missing_ok=True)
            files.append(detail)
        summary = {"succeeded": succeeded, "skipped": skipped, "failed": failed, "files": files, "verified": succeeded > 0}
        if succeeded == 0:
            summary["error"] = "NO_DOCUMENTS_CHANGED"
            summary["message"] = "压缩包中没有成功修改的 Word 文件。"
            return "failed", summary
        if len(archive.infolist()) < MAX_ZIP_FILES:
            result.writestr(manifest_name, json.dumps(summary, ensure_ascii=False).encode())
    with checked_zip(output, ".zip"):
        pass
    return ("partial" if failed or skipped else "completed"), summary


def perform(action: str, args: dict, source: Path | None, input_name: str, output: Path, directory: Path, images: dict[str, Path] | None = None):
    if action == "create_docx":
        result, name = create_docx(args, output)
    elif action == "create_xlsx":
        result, name = create_xlsx(args, output)
    elif action == "create_pptx":
        result, name = create_pptx(args, output)
    elif action == "template_pptx":
        result, name = create_pptx(args, output, source)
    elif action == "read_docx":
        return "completed", read_docx(source, args), None
    elif action == "read_xlsx":
        return "completed", read_xlsx(source, args), None
    elif action == "read_pptx":
        return "completed", read_pptx(source, args), None
    elif action == "render_docx_pdf":
        from .render import render_docx_pdf

        result = render_docx_pdf(source, output, directory)
        name = file_name(f"{Path(input_name).stem}.pdf", ".pdf")
    elif action == "edit_docx":
        result = edit_docx(source, output, validate_edits(args))
        name = file_name(args.get("name") or input_name, ".docx")
        if not all(result["replacements"]):
            return "partial", result, name
    elif action == "recalc_xlsx":
        from .calc import recalculate

        result = recalculate(source, output, directory)
        name = file_name(args.get("name") or input_name, ".xlsx")
        if result["formula_errors"]:
            return "partial", result, name
    elif action == "edit_xlsx":
        result = edit_xlsx(source, output, args)
        name = file_name(args.get("name") or input_name, ".xlsx")
    elif action == "edit_pptx":
        result = edit_pptx(source, output, args, images or {})
        name = file_name(args.get("name") or input_name, ".pptx")
        if result.get("unmatched"):
            if not (any(result["replacements"]) or args.get("add_slides") or args.get("remove_slides") or args.get("replace_images")):
                raise OfficeError("没有找到待替换的内容。", "NO_MATCH")
            return "partial", result, name
    elif action == "batch_edit_docx":
        name = file_name(args.get("name") or f"{Path(input_name).stem}-已修改.zip", ".zip")
        status, result = batch_edit_docx(source, output, directory, args)
        return status, result, name if status != "failed" else None
    else:
        raise OfficeError("未知办公操作。", "INVALID_ARGUMENT")
    return "completed", result, name
