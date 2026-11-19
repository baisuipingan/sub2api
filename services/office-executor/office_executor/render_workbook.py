"""Fixed XLSX preview helper for the networkless office task container only.

Usage: python -m office_executor.render_workbook output/report.xlsx
Never import or invoke this renderer on the Go server or trusted supervisor.
"""
from __future__ import annotations

import argparse
from pathlib import Path
import re
import subprocess
import tempfile

from openpyxl import load_workbook

from .contracts import logical_path
from .runner import check_public_type

MAX_BYTES = 24 * 1024 * 1024
MAX_PAGES = 16


def render_workbook(relative: str) -> dict:
    relative = logical_path(relative)
    if not relative.startswith("output/") or Path(relative).suffix.lower() != ".xlsx":
        raise ValueError("preview accepts an XLSX in the output zone")
    source = Path(relative)
    if source.is_symlink() or not source.is_file() or source.stat().st_size > MAX_BYTES:
        raise ValueError("preview source must be a bounded regular XLSX")
    content = source.read_bytes()
    check_public_type(relative, content)
    destination = Path("scratch/preview") / source.relative_to("output")
    destination.mkdir(mode=0o700, parents=True, exist_ok=True)
    pdf = destination / (source.stem + ".pdf")
    pdf.unlink(missing_ok=True)
    # Restrict export layout in a temporary COPY, preserving the original
    # workbook's formulas, cached values, styles and printing preferences.
    with tempfile.TemporaryDirectory(prefix="xlsx-preview-", dir="scratch/.tmp") as temporary:
        temporary = Path(temporary)
        book = load_workbook(source, data_only=False, keep_links=False)
        try:
            visible = [sheet for sheet in book.worksheets if sheet.sheet_state == "visible"]
            if not visible or len(visible) > 16:
                raise ValueError("preview supports one to sixteen visible worksheets")
            for sheet in visible:
                if sheet.max_row > 20000 or sheet.max_column > 512:
                    raise ValueError("worksheet exceeds bounded preview dimensions")
                sheet.sheet_properties.pageSetUpPr.fitToPage = True
                sheet.page_setup.fitToWidth = 1
                sheet.page_setup.fitToHeight = 1
                sheet.page_setup.paperSize = sheet.PAPERSIZE_A3
                sheet.page_setup.orientation = "landscape"
            staged = temporary / source.name
            book.save(staged)
        finally:
            book.close()
        profile = temporary / "lo-profile"
        process = subprocess.run([
            "soffice", "--headless", "--norestore", "--invisible",
            "-env:SingleAppInstance=false", "-env:UserInstallation=" + profile.resolve().as_uri(),
            "--convert-to", "pdf:calc_pdf_Export", "--outdir", str(destination.resolve()), str(staged.resolve()),
        ], capture_output=True, text=True, timeout=90)
        if process.returncode or not pdf.is_file() or pdf.stat().st_size > 8 * 1024 * 1024:
            raise ValueError("LibreOffice did not produce a bounded PDF preview")
    information = subprocess.run(["pdfinfo", str(pdf)], capture_output=True, text=True, check=True, timeout=15).stdout
    match = re.search(r"^Pages:\s+(\d+)$", information, re.MULTILINE)
    if match is None or not 1 <= int(match.group(1)) <= MAX_PAGES:
        pdf.unlink(missing_ok=True)
        raise ValueError("workbook preview exceeds sixteen pages")
    subprocess.run(["pdftoppm", "-f", "1", "-l", "1", "-singlefile", "-png", "-scale-to", "1280",
                    str(pdf), str(destination / "page-01")], capture_output=True, check=True, timeout=45)
    return {"pdf": str(pdf), "first_page": str(destination / "page-01.png"), "pages": int(match.group(1)),
            "note": "预览为适配纸张的只读副本；原始工作簿和公式保持不变"}


def main():
    parser = argparse.ArgumentParser(description="Render a bounded, read-only XLSX preview inside the task sandbox")
    parser.add_argument("path")
    args = parser.parse_args()
    print(render_workbook(args.path))


if __name__ == "__main__":
    main()
