#!/usr/bin/env python
# Modified by MiloRouter, 2026-11-18: use bounded Poppler rendering, no PyMuPDF.
"""Render a .pptx to one PNG per slide, for visual QA.

Uses preinstalled LibreOffice and Poppler with a private profile per task.
No PyMuPDF dependency is required. It runs only inside the isolated executor.

Usage:
    python skills/bisheng-pptx/scripts/render_deck.py output/deck.pptx
    python skills/bisheng-pptx/scripts/render_deck.py output/deck.pptx --pages 1-4 --width 1600

Prints the workspace-relative PNG paths; pass them to read_file to look at them.
"""

from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
import tempfile
import traceback

import re

DEFAULT_WIDTH_PX = 1280
SOFFICE_TIMEOUT_S = 300


def _find_soffice() -> str | None:
    for name in ("soffice", "libreoffice"):
        found = shutil.which(name)
        if found:
            return found
    return None


def _to_pdf(pptx_path: str, out_dir: str) -> str | None:
    """Convert with a private LibreOffice profile; returns the pdf path or None."""
    soffice = _find_soffice()
    if not soffice:
        print("找不到 soffice/libreoffice，无法渲染预览。跳过视觉 QA，以 inspect_deck.py 的体检为准。")
        return None

    abs_input = os.path.abspath(pptx_path)
    os.makedirs(out_dir, exist_ok=True)
    expected = os.path.join(out_dir, os.path.splitext(os.path.basename(abs_input))[0] + ".pdf")

    with tempfile.TemporaryDirectory() as profile:
        command = [
            soffice,
            "--headless",
            "-env:SingleAppInstance=false",
            f"-env:UserInstallation=file://{profile}",
            "--convert-to",
            "pdf",
            "--outdir",
            out_dir,
            abs_input,
        ]
        try:
            proc = subprocess.run(command, capture_output=True, text=True, timeout=SOFFICE_TIMEOUT_S)
        except subprocess.TimeoutExpired:
            print(f"soffice 转换超时（>{SOFFICE_TIMEOUT_S}s）。跳过视觉 QA。")
            return None

    if not os.path.exists(expected):
        # soffice exits 0 even when it converted nothing, so trust the file, not rc.
        message = (proc.stdout + proc.stderr).strip()
        print(f"soffice 未产出 PDF（rc={proc.returncode}）。输出: {message[:400]}")
        if "could not be loaded" in message:
            print("多半是这套环境的 LibreOffice 没装 Impress 组件（只有 Writer），无法打开 .pptx。")
        print("跳过视觉 QA 即可，不影响 .pptx 交付；以 inspect_deck.py 的体检结果为准。")
        return None
    return expected


def _parse_pages(spec: str | None, total: int) -> list[int]:
    if not spec:
        return list(range(total))
    wanted: set[int] = set()
    for chunk in spec.split(","):
        chunk = chunk.strip()
        if not chunk:
            continue
        if "-" in chunk:
            start, end = chunk.split("-", 1)
            wanted.update(range(int(start) - 1, int(end)))
        else:
            wanted.add(int(chunk) - 1)
    return sorted(p for p in wanted if 0 <= p < total)


def main() -> int:
    parser = argparse.ArgumentParser(description="Render pptx slides to PNG for visual QA")
    parser.add_argument("pptx", help="路径，相对工作区根，例如 output/deck.pptx")
    parser.add_argument("--outdir", default=None, help="默认 scratch/preview/<文件名>/")
    parser.add_argument("--pages", default=None, help="页码，如 1-4 或 2,5,7（默认全部）")
    parser.add_argument("--width", type=int, default=DEFAULT_WIDTH_PX, help=f"输出宽度 px（默认 {DEFAULT_WIDTH_PX}）")
    args = parser.parse_args()

    if not os.path.exists(args.pptx):
        print(f"[FATAL] 文件不存在: {args.pptx}（请用相对工作区根的路径，例如 output/deck.pptx，不要带前导斜杠）")
        return 0

    # Always exit 0, including on a crash: a non-zero exit makes the code
    # interpreter return stderr and drop stdout, hiding the reason.
    try:
        render(args.pptx, args.outdir, args.pages, args.width)
    except Exception:
        print("[FATAL] 渲染预览时出错：")
        traceback.print_exc(file=sys.stdout)
        print("视觉 QA 是可选步骤：跳过它，以 inspect_deck.py 的体检结果为准，不要因此重做 .pptx。")
    return 0


def render(pptx_path: str, outdir: str | None, pages: str | None, width_px: int) -> None:
    stem = os.path.splitext(os.path.basename(pptx_path))[0]
    name = pptx_path.removeprefix("output/") if pptx_path.startswith("output/") else os.path.basename(pptx_path)
    out_dir = outdir or os.path.join("scratch", "preview", name)
    os.makedirs(out_dir, exist_ok=True)

    # Stale frames from a previous run read as "my fix did not apply".
    for name in os.listdir(out_dir):
        if name.startswith("slide-") and name.endswith(".png"):
            os.remove(os.path.join(out_dir, name))

    pdf_path = _to_pdf(pptx_path, out_dir)
    if not pdf_path:
        return

    if not 320 <= width_px <= 2400:
        raise ValueError("preview width must be between 320 and 2400")
    pdfinfo, pdftoppm = shutil.which("pdfinfo"), shutil.which("pdftoppm")
    if not pdfinfo or not pdftoppm:
        raise RuntimeError("Poppler pdfinfo and pdftoppm are required")
    info = subprocess.run([pdfinfo, pdf_path], check=True, capture_output=True,
                          text=True, timeout=30).stdout
    count = re.search(r"^Pages:\s+(\d+)", info, re.MULTILINE)
    if count is None:
        raise RuntimeError("cannot determine PDF page count")
    written = []
    for index in _parse_pages(pages, int(count.group(1))):
        prefix = os.path.join(out_dir, f"slide-{index + 1}")
        subprocess.run([pdftoppm, "-f", str(index + 1), "-l", str(index + 1),
                        "-singlefile", "-png", "-scale-to-x", str(width_px),
                        "-scale-to-y", "-1", pdf_path, prefix],
                       check=True, capture_output=True, timeout=180)
        written.append(prefix + ".png")

    print(f"已渲染 {len(written)} 页（源 PDF: {pdf_path}）:")
    for path in written:
        print(f"  {path}")
    print("预览已生成；若当前任务提供 inspect_rendered_image 工具，请先缩图到工具上限后实际读图复查。")
    print("只有实际读图才可声称完成视觉检查；无该工具时仅报告结构与渲染检查。")
    print("看图时重点看：文字是否溢出/被截断、元素是否重叠、留白是否失衡。")
    print("注意：预装中文字体与用户 PowerPoint 中的实际字体宽度可能不同 ——")
    print("      预览里的文字松紧只作参考，容器请留约 10% 余量，不要为了预览效果反复微调字号。")


if __name__ == "__main__":
    sys.exit(main())
