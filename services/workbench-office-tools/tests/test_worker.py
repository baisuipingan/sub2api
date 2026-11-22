from base64 import b64encode
from contextlib import contextmanager
from datetime import datetime
from hashlib import md5, sha256
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from io import BytesIO
import json
import os
import random
import shutil
from pathlib import Path
from tempfile import TemporaryDirectory
import threading
import time
from urllib.error import HTTPError
from urllib.request import Request, urlopen
import unittest
import zipfile

from docx import Document
from openpyxl import load_workbook
from PIL import Image
from pptx import Presentation
from pptx.dml.color import RGBColor
from pptx.util import Inches

from workbench_office_tools.files import OfficeError
from workbench_office_tools.operations import perform
from workbench_office_tools.server import JobService, OfficeHTTP


@contextmanager
def serving(server):
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield f"http://127.0.0.1:{server.server_address[1]}"
    finally:
        server.shutdown()
        thread.join()
        server.server_close()


class ToolTest(unittest.TestCase):
    def setUp(self):
        self.temp = TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.output = self.root / "output"

    def operation(self, name, args, source=None):
        return perform(name, args, source, source.name if source else "", self.output, self.root)

    def test_docx_scopes_split_runs_and_preserves_format(self):
        document = Document()
        title = document.add_paragraph("旧公司", style="Title")
        title.runs[0].bold = True
        document.core_properties.title = "旧公司"
        paragraph = document.add_paragraph()
        paragraph.add_run("前旧").italic = True
        paragraph.add_run("公司后")
        document.sections[0].header.paragraphs[0].text = "旧公司页眉"
        document.sections[0].footer.paragraphs[0].text = "旧公司页脚"
        source = self.root / "sample.docx"
        document.save(source)
        edits = {
            "edits": [
                {"old_text": "旧公司", "new_text": "新机构", "scope": "title"},
                {"old_text": "旧公司", "new_text": "北京分部", "scope": "body"},
                {"old_text": "旧公司", "new_text": "新机构", "scope": "header"},
                {"old_text": "旧公司", "new_text": "新机构", "scope": "footer"},
            ]
        }
        status, summary, name = self.operation("edit_docx", edits, source)
        self.assertEqual(("completed", [2, 1, 1, 1], "sample.docx"), (status, summary["replacements"], name))
        updated = Document(self.output)
        self.assertEqual(updated.core_properties.title, "新机构")
        self.assertTrue(updated.paragraphs[0].runs[0].bold)
        self.assertEqual(updated.paragraphs[1].text, "前北京分部后")
        self.assertTrue(updated.paragraphs[1].runs[0].italic)
        self.assertEqual(updated.sections[0].header.paragraphs[0].text, "新机构页眉")
        self.assertEqual(updated.sections[0].footer.paragraphs[0].text, "新机构页脚")
        _, read, _ = self.operation("read_docx", {"scope": "title"}, source)
        self.assertEqual(2, len(read["sections"]))

    def test_batch_keeps_original_for_failed_files_and_manifests_outcomes(self):
        self.operation("create_docx", {"name": "输入.docx", "paragraphs": ["旧公司报告"]})
        source = self.root / "原件.zip"
        with zipfile.ZipFile(source, "w") as archive:
            archive.write(self.output, "a/one.docx")
            archive.writestr("b/bad.docx", b"bad document")
            archive.writestr("notes.txt", "keep")
        edits = {"edits": [{"old_text": "旧公司", "new_text": "新机构"}]}
        status, summary, name = self.operation("batch_edit_docx", edits, source)
        self.assertEqual(("partial", 1, 1, 1, "原件-已修改.zip"), (status, summary["succeeded"], summary["failed"], summary["skipped"], name))
        with zipfile.ZipFile(self.output) as archive:
            self.assertEqual(b"bad document", archive.read("b/bad.docx"))
            self.assertEqual(b"keep", archive.read("notes.txt"))
            self.assertEqual(summary, json.loads(archive.read("workbench-result.json")))
            changed = Document(BytesIO(archive.read("a/one.docx")))
            self.assertEqual("新机构报告", changed.paragraphs[0].text)
        with zipfile.ZipFile(source, "w") as archive:
            archive.writestr("../outside.docx", "blocked")
        with self.assertRaises(OfficeError):
            self.operation("batch_edit_docx", edits, source)

    def test_xlsx_uses_values_formulas_and_styles(self):
        self.operation("create_xlsx", {"sheets": [{"name": "月报", "rows": [["项目", 3], ["合计", "=SUM(B1)"]]}]})
        source = self.root / "预算.xlsx"
        self.output.replace(source)
        _, read, _ = self.operation("read_xlsx", {"sheet": "月报", "range": "A1:B2"}, source)
        self.assertEqual([["项目", 3], ["合计", "=SUM(B1)"]], read["rows"])
        _, edit, _ = self.operation("edit_xlsx", {
            "cells": [{"sheet": "月报", "cell": "B1", "value": 10.25, "format": "0.00", "bold": True}],
        }, source)
        self.assertEqual(1, edit["cells_changed"])
        with self.output.open("rb") as output:
            workbook = load_workbook(output)
            worksheet = workbook["月报"]
            self.assertEqual((10.25, "0.00", True), (worksheet["B1"].value, worksheet["B1"].number_format, worksheet["B1"].font.bold))
            self.assertEqual("=SUM(B1)", worksheet["B2"].value)

    def test_xlsx_rectangular_values_and_style_edits(self):
        self.operation("create_xlsx", {"sheets": [{"name": "费用", "rows": [["保留", 2], ["原值"], ["仍保留"]]}]})
        source = self.root / "预算.xlsx"
        self.output.replace(source)
        args = {
            "cells": [{"sheet": "费用", "cell": "D1", "value": "核对"}],
            "ranges": [
                {"sheet": "费用", "range": "B2:C3", "values": [[1.5, True], ["=SUM(B1:B2)", ""]], "format": "0.00"},
                {"sheet": "费用", "range": "B3:C3", "bold": True},
                {"sheet": "费用", "range": "D2:E2", "values": [[46_023, 46_024]], "format": "yyyy-mm-dd"},
            ],
        }
        _, result, _ = self.operation("edit_xlsx", args, source)
        self.assertEqual((7, 3), (result["cells_changed"], result["ranges_changed"]))
        with self.output.open("rb") as handle:
            result_book = load_workbook(handle)
            self.addCleanup(result_book.close)
            sheet = result_book["费用"]
            self.assertEqual(["保留", 1.5, True, "=SUM(B1:B2)", None, "核对"], [sheet[cell].value for cell in ("A1", "B2", "C2", "B3", "C3", "D1")])
            self.assertEqual("0.00", sheet["B3"].number_format)
            self.assertTrue(sheet["B3"].font.bold)
            self.assertFalse(sheet["B2"].font.bold)
            self.assertEqual(datetime(2026, 1, 1), sheet["D2"].value)
        args["ranges"][0]["values"] = [[1, 2], [3]]
        with self.assertRaisesRegex(OfficeError, "行列数"):
            self.operation("edit_xlsx", args, source)

    @unittest.skipUnless(shutil.which("soffice") or Path("/Applications/LibreOffice.app/Contents/MacOS/soffice").exists(), "Calc is optional")
    def test_xlsx_calculates_real_values_in_original_file_structure(self):
        self.operation("create_xlsx", {"sheets": [{"name": "指标", "rows": [[10], [20], ["=SUM(A1:A2)"], ["=1/0"]]}]})
        source = self.root / "计算.xlsx"
        self.output.replace(source)
        status, result, _ = self.operation("recalc_xlsx", {}, source)
        self.assertEqual(("partial", 2, 2), (status, result["formulas"], result["calculated"]))
        self.assertEqual([{"sheet": "指标", "cell": "A4", "error": "#DIV/0!"}], result["formula_errors"])
        with self.output.open("rb") as handle:
            cached = load_workbook(handle, data_only=True)
            self.assertEqual(30, cached["指标"]["A3"].value)
        with self.output.open("rb") as handle:
            formula = load_workbook(handle)
            self.assertEqual("=SUM(A1:A2)", formula["指标"]["A3"].value)

    def test_recalculation_blocks_external_inputs(self):
        self.operation("create_xlsx", {"sheets": [{"name": "test", "rows": [['=WEBSERVICE("https://example.test/docs")']]}]})
        source = self.root / "workbook.xlsx"
        self.output.replace(source)
        with self.assertRaises(OfficeError) as raised:
            self.operation("recalc_xlsx", {}, source)
        self.assertEqual("UNSUPPORTED_FORMULA", raised.exception.code)

    def test_pptx_edit_retains_template_and_can_append_remove(self):
        self.operation("create_pptx", {"slides": [{"title": "旧公司", "text": "业务报告"}, {"title": "待移除"}, {"title": "后续保留"}]})
        source = self.root / "模板.pptx"
        self.output.replace(source)
        _, edit, _ = self.operation("edit_pptx", {
            "replacements": [{"old_text": "旧公司", "new_text": "新机构", "slide": 1}],
            "remove_slides": [2], "add_slides": [{"title": "结论"}],
        }, source)
        self.assertEqual([1], edit["replacements"])
        output = Presentation(self.output)
        self.assertEqual(["新机构", "后续保留", "结论"], [slide.shapes.title.text for slide in output.slides])
        _, details, _ = self.operation("read_pptx", {"offset": 1}, source)
        self.assertEqual(2, details["slides"][0]["number"])

    def test_pptx_creates_from_template_layout(self):
        deck = Presentation()
        deck.slide_width = Inches(13)
        layout = deck.slide_layouts[1]
        layout.name = "公司模板"
        layout.background.fill.solid()
        layout.background.fill.fore_color.rgb = RGBColor(0x15, 0x36, 0x85)
        deck.slides.add_slide(layout).shapes.title.text = "示例页面"
        source = self.root / "公司模板.pptx"
        deck.save(source)
        args = {"name": "投标方案.pptx", "slides": [{"layout": 1, "title": "新方案", "text": "计划说明"}]}
        _, result, name = self.operation("template_pptx", args, source)
        self.assertEqual((1, 0, "投标方案.pptx"), (result["slides"], result["template_slides_kept"], name))
        output_deck = Presentation(self.output)
        self.assertEqual(Inches(13), output_deck.slide_width)
        output_slide = output_deck.slides[0]
        self.assertEqual("新方案", output_slide.shapes.title.text)
        self.assertEqual("公司模板", output_slide.slide_layout.name)
        self.assertEqual(RGBColor(0x15, 0x36, 0x85), output_slide.slide_layout.background.fill.fore_color.rgb)
        _, result, _ = self.operation("template_pptx", {**args, "keep_template_slides": True}, source)
        self.assertEqual(1, result["template_slides_kept"])
        self.assertEqual(["示例页面", "新方案"], [slide.shapes.title.text for slide in Presentation(self.output).slides])

    def test_pptx_replaces_one_shared_picture_without_moving_it(self):
        def png(color: str) -> bytes:
            image = BytesIO()
            Image.new("RGB", (100, 50), color).save(image, "PNG")
            return image.getvalue()

        original = png("red")
        replacement = png("blue")
        deck = Presentation()
        slide = deck.slides.add_slide(deck.slide_layouts[1])
        picture = slide.shapes.add_picture(BytesIO(original), Inches(1), Inches(2), width=Inches(3), height=Inches(1))
        picture.name = "产品图"
        picture.rotation = 20
        picture.crop_left = 0.25
        duplicate = slide.shapes.add_picture(BytesIO(original), Inches(6), Inches(1))
        duplicate.name = "图例"
        source = self.root / "产品展示.pptx"
        deck.save(source)
        _, read, _ = self.operation("read_pptx", {}, source)
        self.assertEqual([{"shape": 3, "name": "产品图"}, {"shape": 4, "name": "图例"}], read["slides"][0]["pictures"])
        self.assertEqual({"index": 1, "name": deck.slide_layouts[1].name}, read["layouts"][1])
        _, result, _ = self.operation("edit_pptx", {"replace_images": [{
            "slide": 1, "shape": 3, "image_base64": b64encode(replacement).decode("ascii"),
        }]}, source)
        self.assertEqual(1, result["images_replaced"])
        updated = Presentation(self.output).slides[0].shapes
        self.assertEqual(replacement, updated[2].image.blob)
        self.assertEqual(original, updated[3].image.blob)
        geometry = ("shape_id", "name", "left", "top", "width", "height", "rotation", "crop_left")
        self.assertEqual([getattr(picture, attr) for attr in geometry], [getattr(updated[2], attr) for attr in geometry])


class WorkerHTTPTest(unittest.TestCase):
    token = "test-office-worker-token-0123"

    def request(self, origin, path, method="GET", payload=None, auth=True, binary=False, node_id=None):
        data = json.dumps(payload).encode() if payload is not None else None
        headers = {"Authorization": "Bearer " + self.token} if auth else {}
        if node_id is not None:
            headers["X-Office-Node-ID"] = node_id
        request = Request(origin + path, data=data, method=method, headers=headers)
        try:
            response = urlopen(request, timeout=5)
        except HTTPError as exc:
            response = exc
        with response:
            raw = response.read()
            return response.status, raw if binary else json.loads(raw)

    def wait_for(self, origin, path):
        for _ in range(120):
            code, receipt = self.request(origin, path)
            self.assertEqual(200, code)
            if receipt["status"] != "running":
                return receipt
            time.sleep(.05)
        self.fail("worker failed to settle")

    def test_updates_concurrency_in_place_and_drains_existing_jobs(self):
        release_jobs = threading.Event()
        jobs_started = threading.Barrier(4)

        def complete_after_release(*_):
            if not release_jobs.is_set():
                jobs_started.wait(timeout=5)
                if not release_jobs.wait(5):
                    raise TimeoutError("test did not release the existing jobs")
            return {"status": "completed"}, None

        with TemporaryDirectory() as temp:
            state_dir = Path(temp)
            service = JobService(state_dir, set(), 1)
            service.run_operation = complete_after_release
            try:
                with serving(OfficeHTTP(("127.0.0.1", 0), service, self.token)) as worker:
                    identity = service.node_id
                    update = "/v1/concurrency"
                    increase = {"concurrency": 3, "revision": 4}
                    self.assertEqual(0, self.request(worker, "/v1/status")[1]["concurrency_revision"])
                    self.assertEqual(401, self.request(worker, update, "PUT", increase, auth=False, node_id=identity)[0])
                    self.assertEqual(409, self.request(worker, update, "PUT", increase)[0])
                    self.assertEqual(409, self.request(worker, update, "PUT", increase, node_id="other")[0])
                    self.assertEqual(400, self.request(worker, update, "PUT", {**increase, "revision": True}, node_id=identity)[0])
                    code, applied = self.request(worker, update, "PUT", increase, node_id=identity)
                    self.assertEqual((200, 3, 4), (code, applied["concurrency"], applied["concurrency_revision"]))

                    payload = {"action": "create_docx", "args": {}}
                    jobs = [f"/v1/jobs/parallel-{i}" for i in range(3)]
                    for path in jobs:
                        self.assertEqual(202, self.request(worker, path, "POST", payload, node_id=identity)[0])
                    jobs_started.wait(timeout=5)
                    decrease = {"concurrency": 1, "revision": 5}
                    code, applied = self.request(worker, update, "PUT", decrease, node_id=identity)
                    self.assertEqual((200, 1, 5, 3), (code, applied["concurrency"], applied["concurrency_revision"], applied["running"]))
                    for update_attempt in (increase, {**decrease, "concurrency": 8}, decrease):
                        self.assertEqual((200, applied), self.request(worker, update, "PUT", update_attempt, node_id=identity))
                    blocked = "/v1/jobs/blocked"
                    code, receipt = self.request(worker, blocked, "POST", payload, node_id=identity)
                    self.assertEqual((503, "WORKER_BUSY"), (code, receipt["error"]))
                    release_jobs.set()
                    for path in jobs:
                        self.assertEqual("completed", self.wait_for(worker, path)["status"])
                    with service.execution_slots:
                        self.assertTrue(service.execution_slots.wait_for(lambda: not service.active, timeout=5))
                    later = "/v1/jobs/later"
                    self.assertEqual(202, self.request(worker, later, "POST", payload, node_id=identity)[0])
                    self.assertEqual("completed", self.wait_for(worker, later)["status"])
            finally:
                release_jobs.set()
                service.close()

            restarted = JobService(state_dir, set(), 1)
            try:
                self.assertEqual(identity, restarted.status()["node_id"])
                self.assertEqual(0, restarted.status()["concurrency_revision"])
                self.assertEqual(5, restarted.configure_concurrency(decrease)["concurrency_revision"])
            finally:
                restarted.close()

    def test_pptx_image_attachment_reuses_cached_version_and_checks_hash(self):
        replacement = BytesIO()
        image_bytes = random.Random(42).randbytes(320 * 240 * 3)
        Image.frombytes("RGB", (320, 240), image_bytes).save(replacement, "PNG")
        replacement_data = replacement.getvalue()
        self.assertGreater(len(replacement_data), 180_000)
        original = BytesIO()
        Image.new("RGB", (32, 64), "red").save(original, "PNG")
        original.seek(0)
        deck = Presentation()
        slide = deck.slides.add_slide(deck.slide_layouts[1])
        slide.shapes.add_picture(original, Inches(1), Inches(2), width=Inches(3), height=Inches(1))
        deck_bytes = BytesIO()
        deck.save(deck_bytes)
        source_data = deck_bytes.getvalue()
        reads = []

        class Storage(BaseHTTPRequestHandler):
            def log_message(self, *_args):
                pass

            def do_GET(self):
                reads.append(self.path)
                data = source_data if self.path.startswith("/deck?") else replacement_data
                self.send_response(200)
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

        with TemporaryDirectory() as temp, serving(ThreadingHTTPServer(("127.0.0.1", 0), Storage)) as storage:
            service = JobService(Path(temp), {storage}, 1)
            try:
                with serving(OfficeHTTP(("127.0.0.1", 0), service, self.token)) as worker:
                    image_version = "b" * 32
                    attachment = {
                        "version_id": image_version, "name": "实拍.png", "bytes": len(replacement_data),
                        "sha256": sha256(replacement_data).hexdigest(), "url": storage + "/image?signature=one",
                    }
                    payload = {
                        "action": "edit_pptx",
                        "input": {"version_id": "a" * 32, "name": "演示.pptx", "bytes": len(source_data),
                            "sha256": sha256(source_data).hexdigest(), "url": storage + "/deck?signature=one"},
                        "args": {"replace_images": [{"slide": 1, "shape": 3, "image_version_id": image_version}]},
                        "images": [attachment],
                    }
                    path = "/v1/jobs/picture1"
                    self.assertEqual(202, self.request(worker, path, "POST", payload)[0])
                    receipt = self.wait_for(worker, path)
                    self.assertEqual("completed", receipt["status"])
                    _, raw = self.request(worker, path + "/output", binary=True)
                    self.assertEqual(replacement_data, Presentation(BytesIO(raw)).slides[0].shapes[2].image.blob)

                    attachment["url"] = storage + "/image?signature=two"
                    self.assertEqual((200, receipt), self.request(worker, path, "POST", payload))
                    second = "/v1/jobs/picture2"
                    self.assertEqual(202, self.request(worker, second, "POST", payload)[0])
                    self.assertEqual("completed", self.wait_for(worker, second)["status"])
                    self.assertEqual(["/deck?signature=one", "/image?signature=one"], reads)

                    attachment["sha256"] = "0" * 64
                    self.assertEqual(409, self.request(worker, path, "POST", payload)[0])
                    invalid = "/v1/jobs/invalid-picture"
                    self.assertEqual(202, self.request(worker, invalid, "POST", payload)[0])
                    self.assertEqual("INPUT_HASH_MISMATCH", self.wait_for(worker, invalid)["error"])

                    payload["images"] = []
                    self.assertEqual(400, self.request(worker, "/v1/jobs/missing-image", "POST", payload)[0])
            finally:
                service.close()

    def test_http_idempotency_signed_input_cache_and_recovery(self):
        with TemporaryDirectory() as temp:
            staging = Path(temp)
            source = staging / "input.docx"
            document = Document()
            document.add_paragraph("关键信息")
            document.save(source)
            data = source.read_bytes()
            reads = []
            uploads = {}
            upload_conditions = []
            allow_slow_read = threading.Event()

            class MockStorage(BaseHTTPRequestHandler):
                def log_message(self, *_args):
                    pass

                def do_PUT(self):
                    body = self.rfile.read(int(self.headers["Content-Length"]))
                    checksum = b64encode(md5(body, usedforsecurity=False).digest()).decode("ascii")
                    if self.headers.get("Content-MD5") != checksum:
                        self.send_error(400)
                        return
                    upload_conditions.append(self.headers.get("If-None-Match"))
                    if self.path in uploads and self.headers.get("If-None-Match") == "*":
                        self.send_response(412)
                        self.send_header("Content-Length", "0")
                        self.end_headers()
                        return
                    uploads[self.path] = (self.headers.get("Content-Type"), body)
                    self.send_response(200)
                    self.send_header("Content-Length", "0")
                    self.end_headers()

                def do_GET(self):
                    reads.append(self.path)
                    if self.path == "/slow":
                        allow_slow_read.wait(10)
                    self.send_response(200)
                    self.send_header("Content-Length", str(len(data)))
                    self.end_headers()
                    self.wfile.write(data)

            storage = ThreadingHTTPServer(("127.0.0.1", 0), MockStorage)
            with serving(storage) as storage_url:
                state = staging / "state"
                service = JobService(state, {storage_url}, 1)
                try:
                    with serving(OfficeHTTP(("127.0.0.1", 0), service, self.token)) as worker:
                        path = "/v1/jobs/example1"
                        args = {"action": "read_docx", "input": {
                            "name": "input.docx", "version_id": "a" * 32, "sha256": sha256(data).hexdigest(),
                            "bytes": len(data), "url": storage_url + "/read?temporary=1",
                        }}
                        self.assertEqual(401, self.request(worker, path, "POST", args, auth=False)[0])
                        self.assertEqual(202, self.request(worker, path, "POST", args)[0])
                        receipt = self.wait_for(worker, path)
                        self.assertEqual("completed", receipt["status"])
                        self.assertIn("关键信息", receipt["result"]["sections"][0]["text"])
                        args["input"]["url"] = storage_url + "/read?temporary=2"
                        self.assertEqual((200, receipt), self.request(worker, path, "POST", args))
                        path2 = "/v1/jobs/example2"
                        self.assertEqual(202, self.request(worker, path2, "POST", args)[0])
                        self.assertEqual("completed", self.wait_for(worker, path2)["status"])
                        self.assertEqual(["/read?temporary=1"], reads)
                        args["args"] = {"scope": "footer"}
                        self.assertEqual(409, self.request(worker, path, "POST", args)[0])

                        pending = "/v1/jobs/pending"
                        args["input"].update(version_id="b"*32, url=storage_url + "/slow")
                        self.assertEqual(202, self.request(worker, pending, "POST", args)[0])
                        self.assertEqual("cancelled", self.request(worker, pending, "DELETE")[1]["status"])
                        self.assertEqual("cancelled", self.wait_for(worker, pending)["status"])
                        allow_slow_read.set()

                        cancelled_first = "/v1/jobs/cancelled-first"
                        early_payload = {"action": "create_docx", "args": {"paragraphs": ["cancelled"]}}
                        self.assertEqual("cancelled", self.request(worker, cancelled_first, "DELETE")[1]["status"])
                        self.assertEqual("cancelled", self.request(worker, cancelled_first, "POST", early_payload)[1]["status"])
                        path3 = "/v1/jobs/create"
                        payload = {"action": "create_docx", "args": {"paragraphs": ["交付文件"]}}
                        self.assertEqual(202, self.request(worker, path3, "POST", payload)[0])
                        created = self.wait_for(worker, path3)
                        _, output = self.request(worker, path3 + "/output", binary=True)
                        self.assertEqual(created["output"]["sha256"], sha256(output).hexdigest())
                        checksum = b64encode(md5(output, usedforsecurity=False).digest()).decode("ascii")
                        self.assertEqual(checksum, created["output"]["content_md5"])
                        put = {"url": storage_url + "/output?temporary=3", "headers": {"Content-Type": "application/octet-stream", "Content-MD5": "invalid"}}
                        self.assertEqual("OUTPUT_CHECKSUM_MISMATCH", self.request(worker, path3 + "/upload", "POST", put)[1]["error"])
                        put["headers"].update({"Content-MD5": checksum, "If-None-Match": "*"})
                        self.assertEqual((200, created), self.request(worker, path3 + "/upload", "POST", put))
                        self.assertEqual(("application/octet-stream", output), uploads["/output?temporary=3"])
                        version = "c" * 32
                        cached = service.cache / f"{version}-{created['output']['sha256']}"
                        self.assertFalse(cached.exists())
                        put["version_id"] = version
                        self.assertEqual(412, self.request(worker, path3 + "/upload", "POST", put)[0])
                        self.assertFalse(cached.exists())
                        del put["headers"]["If-None-Match"]
                        self.assertEqual((200, created), self.request(worker, path3 + "/upload", "POST", put))
                        self.assertEqual(["*", "*", None], upload_conditions)
                        self.assertTrue(cached.samefile(service.outputs / "create"))
                        earlier = time.time() - 120
                        os.utime(cached, (earlier, earlier))
                        self.assertEqual(200, self.request(worker, path3 + "/upload", "POST", put)[0])
                        self.assertGreater(cached.stat().st_mtime, earlier)
                        before_readback = list(reads)
                        readback = "/v1/jobs/local-readback"
                        readback_payload = {"action": "read_docx", "input": {
                            "name": created["output"]["name"], "version_id": version,
                            "sha256": created["output"]["sha256"], "bytes": created["output"]["bytes"],
                            "url": storage_url + "/should-use-cache",
                        }}
                        self.assertEqual(202, self.request(worker, readback, "POST", readback_payload)[0])
                        self.assertIn("交付文件", self.wait_for(worker, readback)["result"]["sections"][0]["text"])
                        self.assertEqual(before_readback, reads)
                        put["url"] = "https://unconfigured.example/document"
                        self.assertEqual(400, self.request(worker, path3 + "/upload", "POST", put)[0])
                        legacy = {**created, "output": {key: value for key, value in created["output"].items() if key != "content_md5"}}
                        with service.lock:
                            service.connection.execute("UPDATE jobs SET receipt=? WHERE id='create'", (json.dumps(legacy),))
                            service.connection.commit()
                finally:
                    allow_slow_read.set()
                    service.close()
                restarted = JobService(state, {storage_url}, 1)
                try:
                    self.assertEqual(created, restarted.lookup("create"))
                    self.assertEqual(receipt, restarted.lookup("example1"))
                    self.assertEqual("cancelled", restarted.lookup("pending")["status"])
                finally:
                    restarted.close()


if __name__ == "__main__":
    unittest.main()
