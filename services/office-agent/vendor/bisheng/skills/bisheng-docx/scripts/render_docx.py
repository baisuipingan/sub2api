#!/usr/bin/env python3
# Modified by MiloRouter, 2026-11-18: use bounded Poppler rendering, no PyMuPDF.
"""Render a .docx to per-page PNGs so the layout can be eyeballed.

    import subprocess, sys
    r = subprocess.run([sys.executable, "skills/bisheng-docx/scripts/render_docx.py",
                        "output/x.docx"], capture_output=True, text=True)

Flags: ``--dpi 110``, ``--timeout 180``, ``--outdir``.

MiloRouter renders PDF with the preinstalled Poppler pdftoppm/pdfinfo tools.
No PyMuPDF dependency is required.
.docx goes through LibreOffice **Writer** (present in the release image and on
114); only .pptx needs Impress. Optional step either way: it only helps when the
model can actually read images back, and a PNG sent to a model that cannot read
images can fail the whole request. When LibreOffice is absent or cannot load the
file, the script says so plainly and ``inspect_docx.py`` remains the source of
truth.

Always exits 0 — the executor discards stdout on a non-zero exit.
"""

import argparse
import os
import shutil
import subprocess
import sys
import tempfile
import traceback


def find_soffice() -> str | None:
    return shutil.which("soffice") or shutil.which("libreoffice")


def to_pdf(src: str, outdir: str, timeout: int) -> str | None:
    soffice = find_soffice()
    if not soffice:
        print("[跳过] 环境里没有 LibreOffice（soffice），无法渲染预览。")
        print("       这不影响 .docx 的生成与交付 —— 以 inspect_docx.py 的体检结果为准。")
        return None

    os.makedirs(outdir, exist_ok=True)
    # A throwaway profile per run, deleted afterwards: a fixed profile path is
    # what makes concurrent conversions in a multi-task worker corrupt each
    # other, and a leaked one leaves ~0.5MB of junk in /tmp per render.
    with tempfile.TemporaryDirectory(prefix="lo_profile_") as profile:
        cmd = [
            soffice,
            "--headless",
            "--norestore",
            "--invisible",
            "-env:SingleAppInstance=false",
            f"-env:UserInstallation=file://{profile}",
            "--convert-to",
            "pdf",
            "--outdir",
            outdir,
            os.path.abspath(src),
        ]
        try:
            proc = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
        except subprocess.TimeoutExpired:
            print(f"[失败] LibreOffice 转换超时（超过 {timeout}s），没有产出预览图。")
            print("       这只影响可选的视觉检查 —— 以 inspect_docx.py 的体检结果为准，不要因此重做文档。")
            return None

    # soffice exits 0 even when it converted nothing → 只认产出文件，不认退出码。
    pdf = os.path.join(outdir, os.path.splitext(os.path.basename(src))[0] + ".pdf")
    if not os.path.exists(pdf):
        message = ((proc.stdout or "") + (proc.stderr or "")).strip()
        print(f"[跳过] LibreOffice 没能转出 PDF（退出码 {proc.returncode}）：{message[:200]}")
        if "source file could not be loaded" in message.lower():
            print("       这台机器的 LibreOffice 很可能没装 Writer 组件，请让运维补装 libreoffice-writer。")
        elif proc.returncode == 137:
            print("       退出码 137 = 被 OOM killer 杀掉，是内存配额太小，不是 LibreOffice 坏了。")
        return None
    return pdf


def rasterise(pdf: str, outdir: str, dpi: int) -> list[str]:
    tool = shutil.which("pdftoppm")
    if not tool:
        raise RuntimeError("Poppler pdftoppm is required for preview validation")
    if not 36 <= dpi <= 200:
        raise ValueError("preview DPI must be between 36 and 200")
    prefix = os.path.join(outdir, "page")
    # The isolated executor enforces the total wall-time, memory and disk caps.
    subprocess.run([tool, "-png", "-r", str(dpi), pdf, prefix],
                   check=True, capture_output=True, timeout=180)
    pages = sorted((name for name in os.listdir(outdir)
                    if name.startswith("page-") and name.endswith(".png")),
                   key=lambda name: int(name[5:-4]))
    written = []
    pad = max(2, len(str(len(pages))))
    for index, name in enumerate(pages, 1):
        source = os.path.join(outdir, name)
        target = os.path.join(outdir, f"page-{index:0{pad}d}.png")
        if source != target:
            os.replace(source, target)
        written.append(target)
    return written


def main() -> None:
    parser = argparse.ArgumentParser(description="把 .docx 渲染成逐页 PNG")
    parser.add_argument("path")
    parser.add_argument("--dpi", type=int, default=110)
    parser.add_argument("--timeout", type=int, default=180)
    parser.add_argument("--outdir", default=None, help="默认 scratch/preview/<文件名>/")
    args = parser.parse_args()

    if not os.path.exists(args.path):
        print(f"[FATAL] 文件不存在: {args.path}")
        print("提示：用相对路径 `output/x.docx`，不要写 `/output/x.docx`。")
        return

    stem = os.path.splitext(os.path.basename(args.path))[0]
    name = args.path.removeprefix("output/") if args.path.startswith("output/") else os.path.basename(args.path)
    outdir = args.outdir or os.path.join("scratch", "preview", name)

    try:
        os.makedirs(outdir, exist_ok=True)
        pdf = to_pdf(args.path, outdir, args.timeout)
        if not pdf:
            return
        pages = rasterise(pdf, outdir, args.dpi)
        if not pages:
            return
        print(f"已渲染 {len(pages)} 页到 {outdir}/")
        for path in pages:
            print(f"  {path}")
        print()
        print("预览已生成；若当前任务提供 inspect_rendered_image 工具，请先缩图到工具上限后实际读图复查。")
        print("只有实际读图才可声称完成视觉检查；无该工具时仅报告结构与渲染检查。服务器使用预装中文字体，")
        print("和用户 Word 里的实际字体宽度不同 —— 预览里的行长松紧只作参考，")
        print("不要为了预览效果反复微调字号，以 inspect_docx.py 的体检结果为准。")
    except Exception:
        print("[FATAL] 渲染过程本身出错：")
        traceback.print_exc(file=sys.stdout)


if __name__ == "__main__":
    main()
    sys.exit(0)
