"""Additional synthetic office scenarios shared by both model protocols."""

import re
import uuid


TEXT_CONTENT = {
    "txt": "旧公司交付单\n",
    "md": "# 旧公司交付单\n",
    "drawio": (
        '<mxfile><diagram name="第 1 页"><mxGraphModel><root>'
        '<mxCell id="0"/><mxCell id="1" parent="0"/>'
        '<mxCell id="company" value="旧公司" vertex="1" parent="1">'
        '<mxGeometry x="10" y="10" width="80" height="30" as="geometry"/>'
        '</mxCell></root></mxGraphModel></diagram></mxfile>'
    ),
    "py": 'company = "旧公司"\n',
}

FORMAT_STEPS = {
    "xlsx": ("create_xlsx", "read_xlsx", "edit_xlsx", "recalc_xlsx", "read_xlsx"),
    "pptx": ("create_pptx", "read_pptx", "edit_pptx", "read_pptx", "template_pptx", "read_pptx"),
    "pptx_image": ("read_pptx", "edit_pptx", "read_pptx"),
    "zip": ("batch_edit_docx",),
    **{suffix: ("write_text", "read_text", "edit_text", "read_text") for suffix in TEXT_CONTENT},
}

FORMAT_MARKER = re.compile(
    r"\bMILO_NATIVE_FORMAT_(XLSX|PPTX_IMAGE|PPTX|ZIP|TXT|MD|DRAWIO|PY)\b"
    r"((?: [0-9a-f-]{36}){0,2})"
)


def require(predicate, reason):
    if not predicate:
        raise ValueError(reason)


def version(receipt):
    require(receipt["verified"] is True, "tool did not verify its artifact")
    return str(uuid.UUID(receipt["artifact"]["version_id"]))


def format_arguments(format_name, attachments, receipts):
    require(all(not result.get("error") for result in receipts), f"{format_name} tool reported an error")
    index = len(receipts)
    if format_name == "zip":
        require(len(attachments) == 1, "batch fixture requires an attached ZIP version")
        if index == 0:
            return {"version_id": str(uuid.UUID(attachments[0])), "name": "native-batch-edited.zip",
                    "edits": [{"old_text": "旧公司", "new_text": "新公司", "scope": "all"}]}
        version(receipts[0])
        require([receipts[0][key] for key in ("succeeded", "skipped", "failed")] == [1, 1, 1],
                "batch fixture did not preserve partial results")
        return None
    if format_name == "pptx_image":
        require(len(attachments) == 2, "picture replacement needs a PPTX and an image")
        if index == 0:
            return {"version_id": attachments[0]}
        pictures = receipts[0]["slides"][0]["pictures"]
        require(len(pictures) == 1, "source presentation did not have a picture")
        if index == 1:
            return {"version_id": attachments[0], "replace_images": [
                {"slide": 1, "shape": pictures[0]["shape"], "image_version_id": attachments[1]},
            ]}
        output = version(receipts[1])
        require(output != attachments[0] and receipts[1]["images_replaced"] == 1, "picture replacement failed")
        if index == 2:
            return {"version_id": output}
        require(len(receipts[2]["slides"][0]["pictures"]) == 1, "edited slide lost its picture")
        return None

    name = f"native-acceptance.{format_name}"
    if index == 0:
        if format_name == "xlsx":
            return {"name": name, "sheets": [{"name": "交付", "rows": [["企业", "报价"], ["旧公司", 42], ["合计", "=B2*2"]]}]}
        if format_name == "pptx":
            return {"name": name, "slides": [{"title": "旧公司交付单", "text": "旧公司已经交付。"}]}
        return {"name": name, "text": TEXT_CONTENT[format_name]}

    first = version(receipts[0])
    if index >= 2:
        verify_read(format_name, receipts[1], "旧公司")
    if index == 2:
        match = {"old_text": "旧公司", "new_text": "新公司"}
        if format_name == "xlsx":
            return {"version_id": first, "ranges": [{"sheet": "交付", "range": "A2:B2", "values": [["新公司", 42]], "bold": True}]}
        return {"version_id": first, "replacements" if format_name == "pptx" else "edits": [match]}

    if index == 1:
        read = {"version_id": first}
        if format_name == "xlsx":
            read.update(sheet="交付", range="A1:B3")
        return read
    edited = version(receipts[2])
    require(first != edited, "edit reused the original version")
    if format_name == "xlsx":
        require(receipts[2]["ranges_changed"] == 1 and receipts[2]["cells_changed"] == 2, "Excel range not applied")
        if index == 3:
            return {"version_id": edited}
        recalculated = version(receipts[3])
        require(recalculated != edited and receipts[3]["calculated"] == 1 and not receipts[3]["formula_errors"],
                "recalculation did not publish verified formula values")
        if index == 4:
            return {"version_id": recalculated, "sheet": "交付", "range": "A1:B3", "data_only": True}
    elif index == 3:
        return {"version_id": edited}
    elif format_name == "pptx":
        verify_read(format_name, receipts[3], "新公司")
        if index == 4:
            layouts = receipts[3]["layouts"]
            require(len(layouts) > 1 and layouts[1]["index"] == 1, "the presentation did not report its layouts")
            return {"version_id": edited, "name": name, "slides": [{"title": "复用主题", "text": "来自原稿布局。", "layout": 1}]}
        templated = version(receipts[4])
        require(templated != edited and receipts[4]["template_slides_kept"] == 0, "template did not create a clean copy")
        if index == 5:
            return {"version_id": templated}
        published = receipts[5]["slides"]
        require(len(published) == 1 and published[0]["title"] == "复用主题" and "来自原稿布局。" in published[0]["text"],
                "generated template slide did not read back")
        return None
    verify_read(format_name, receipts[-1], "新公司")
    return None


def verify_read(format_name, receipt, company):
    if format_name == "xlsx":
        formula = "=B2*2" if company == "旧公司" else 84
        require(receipt["rows"] == [["企业", "报价"], [company, 42], ["合计", formula]], "Excel read-back did not preserve cells")
    elif format_name == "pptx":
        slides = receipt["slides"]
        require(len(slides) == 1 and slides[0]["title"] == company + "交付单" and company + "已经交付。" in slides[0]["text"],
                "PowerPoint read-back did not preserve slide text")
    else:
        require(receipt["text"] == TEXT_CONTENT[format_name].replace("旧公司", company), "source read-back changed file content")
