"""Render a separate print preview of a Word document."""

from pathlib import Path
import shutil
import subprocess

from .calc import restrict_network
from .files import MAX_FILE, OfficeError, checked_zip


def render_docx_pdf(source: Path, output: Path, directory: Path) -> dict:
    with checked_zip(source, ".docx"):
        pass
    soffice = shutil.which("soffice") or shutil.which("libreoffice")
    if soffice is None:
        app = Path("/Applications/LibreOffice.app/Contents/MacOS/soffice")
        soffice = str(app) if app.exists() else None
    if soffice is None:
        raise OfficeError("办公节点尚未安装 Word 渲染工具。", "PRINT_RENDERER_UNAVAILABLE")
    original = directory / "input.docx"
    shutil.copyfile(source, original)
    converted = directory / "input.pdf"
    profile = directory / "writer-profile"
    config = profile / "user"
    config.mkdir(parents=True)
    (config / "registrymodifications.xcu").write_text(
        '<oor:items xmlns:oor="http://openoffice.org/2001/registry">'
        '<item oor:path="/org.openoffice.Office.Common/Security/Scripting">'
        '<prop oor:name="MacroSecurityLevel" oor:op="fuse"><value>3</value></prop></item>'
        '<item oor:path="/org.openoffice.Office.Writer/Content/Update">'
        '<prop oor:name="Link" oor:op="fuse"><value>0</value></prop></item></oor:items>',
        encoding="utf-8",
    )
    command = [
        soffice, "--headless", "--norestore", f"-env:UserInstallation={profile.as_uri()}",
        "--convert-to", "pdf:writer_pdf_Export", "--outdir", str(directory), str(original),
    ]
    try:
        result = subprocess.run(
            command, env={"LANG": "C.UTF-8", "PATH": "/usr/bin:/bin", "TMPDIR": str(directory)},
            timeout=60, check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
            preexec_fn=restrict_network,
        )
        if result.returncode or not 0 < converted.stat().st_size <= MAX_FILE:
            raise ValueError("invalid converted file")
        with converted.open("rb") as handle:
            if handle.read(5) != b"%PDF-":
                raise ValueError("invalid converted file")
    except (OSError, ValueError, subprocess.SubprocessError) as exc:
        raise OfficeError("这份文档无法生成打印版式。", "PRINT_RENDER_FAILED") from exc
    converted.rename(output)
    return {"verified": True}
