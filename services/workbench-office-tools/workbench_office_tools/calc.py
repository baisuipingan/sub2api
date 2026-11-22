"""Recalculate cached Excel values in a copy, preserving the input OOXML."""

import ctypes
import errno
from pathlib import Path
import shutil
import re
import socket
import subprocess
import sys
import zipfile

from lxml import etree
from openpyxl import load_workbook
from openpyxl.compat import safe_string
from openpyxl.utils.datetime import to_excel

from .files import OfficeError, checked_zip
from .word import xml_part

SHEET = "{http://schemas.openxmlformats.org/spreadsheetml/2006/main}"
OUTSIDE_FORMULA = re.compile(
    r"(?:WEBSERVICE|RTD|DDE|IMAGE|HYPERLINK)\s*\(|(?:https?|file|ftp):|\[[^\]]{1,255}\][^!;]{0,255}!|\|[^!]+!",
    re.IGNORECASE,
)


class SeccompArg(ctypes.Structure):
    _fields_ = [("arg", ctypes.c_uint), ("op", ctypes.c_uint), ("low", ctypes.c_uint64), ("high", ctypes.c_uint64)]


def restrict_network() -> None:
    if not sys.platform.startswith("linux"):
        return
    lib = ctypes.CDLL("libseccomp.so.2", use_errno=True)
    lib.seccomp_init.argtypes = [ctypes.c_uint32]
    lib.seccomp_init.restype = ctypes.c_void_p
    lib.seccomp_syscall_resolve_name.argtypes = [ctypes.c_char_p]
    lib.seccomp_rule_add_array.argtypes = [ctypes.c_void_p, ctypes.c_uint32, ctypes.c_int, ctypes.c_uint, ctypes.POINTER(SeccompArg)]
    lib.seccomp_load.argtypes = [ctypes.c_void_p]
    lib.seccomp_release.argtypes = [ctypes.c_void_p]
    context = lib.seccomp_init(0x7FFF0000)  # SCMP_ACT_ALLOW
    if not context:
        raise RuntimeError("cannot initialize network isolation")
    try:
        call = lib.seccomp_syscall_resolve_name(b"socket")
        for family in (socket.AF_INET, socket.AF_INET6, socket.AF_PACKET, socket.AF_NETLINK):
            equal_family = SeccompArg(0, 4, family, 0)  # SCMP_CMP_EQ
            denied = 0x00050000 | errno.EACCES  # SCMP_ACT_ERRNO
            if lib.seccomp_rule_add_array(context, denied, call, 1, ctypes.byref(equal_family)) != 0:
                raise RuntimeError("cannot configure network isolation")
        if lib.seccomp_load(context) != 0:
            raise RuntimeError("cannot activate network isolation")
    finally:
        lib.seccomp_release(context)


def run_calc(source: Path, directory: Path) -> Path:
    soffice = shutil.which("soffice") or shutil.which("libreoffice")
    if soffice is None and not sys.platform.startswith("linux"):
        macos = Path("/Applications/LibreOffice.app/Contents/MacOS/soffice")
        soffice = str(macos) if macos.exists() else None
    if not soffice:
        raise OfficeError("办公节点暂未配置 Excel 公式重算工具。", "CALC_UNAVAILABLE")
    copy_path = directory / "formula-input.xlsx"
    shutil.copyfile(source, copy_path)
    converted = directory / "calculated"
    converted.mkdir()
    profile = directory / "calc-profile"
    config = profile / "user"
    config.mkdir(parents=True)
    (config / "registrymodifications.xcu").write_text(
        '<oor:items xmlns:oor="http://openoffice.org/2001/registry">'
        '<item oor:path="/org.openoffice.Office.Calc/Content/Update">'
        '<prop oor:name="Link" oor:op="fuse"><value>2</value></prop></item>'
        '<item oor:path="/org.openoffice.Office.Common/Security/Scripting">'
        '<prop oor:name="MacroSecurityLevel" oor:op="fuse"><value>3</value></prop></item></oor:items>',
        encoding="utf-8",
    )
    env = {"LANG": "C.UTF-8", "PATH": "/usr/bin:/bin", "TMPDIR": str(directory)}
    command = [
        soffice, "--headless", "--norestore", f"-env:UserInstallation={profile.as_uri()}",
        "--convert-to", "xlsx:Calc MS Excel 2007 XML", "--outdir", str(converted), str(copy_path),
    ]
    try:
        result = subprocess.run(command, env=env, timeout=60, check=False, capture_output=True, preexec_fn=restrict_network)
    except (OSError, subprocess.TimeoutExpired, subprocess.SubprocessError) as exc:
        raise OfficeError("办公节点未完成 Excel 公式重算。", "CALC_FAILED") from exc
    target = converted / copy_path.name
    if result.returncode or not target.is_file():
        raise OfficeError("办公节点未完成 Excel 公式重算。", "CALC_FAILED")
    return target


def put_value(cell, value, epoch) -> None:
    node = cell.find(SHEET + "v")
    if node is None:
        node = etree.SubElement(cell, SHEET + "v")
    if value.data_type == "e":
        cell.set("t", "e")
        node.text = value.value
    elif isinstance(value.value, bool):
        cell.set("t", "b")
        node.text = "1" if value.value else "0"
    elif isinstance(value.value, str):
        cell.set("t", "str")
        node.text = value.value
    elif value.value is not None:
        from datetime import date, time, timedelta
        cell.set("t", "n")
        raw = to_excel(value.value, epoch) if isinstance(value.value, (date, time, timedelta)) else value.value
        node.text = safe_string(raw)


def recalculate(source: Path, output: Path, directory: Path) -> dict:
    with checked_zip(source, ".xlsx") as archive:
        worksheets = [name for name in archive.namelist() if name.startswith("xl/worksheets/") and name.endswith(".xml") and "/_rels/" not in name]
        for name in archive.namelist():
            if name.startswith("xl/externalLinks/"):
                raise OfficeError("含有外部工作簿链接，无法安全重算。", "UNSUPPORTED_FORMULA")
            if name.endswith(".rels"):
                relations = xml_part(archive.read(name))
                if any(item.get("TargetMode") == "External" for item in relations):
                    raise OfficeError("工作簿引用外部资源，无法安全重算。", "UNSUPPORTED_FORMULA")
            if name in worksheets or name == "xl/workbook.xml":
                root = xml_part(archive.read(name))
                expressions = root.iter(SHEET + "f", SHEET + "definedName")
                if any(OUTSIDE_FORMULA.search(item.text or "") for item in expressions):
                    raise OfficeError("发现引用外部资源的公式，无法安全重算。", "UNSUPPORTED_FORMULA")
    # openpyxl follows workbook relationships to retain arbitrary numbered worksheet names.
    from openpyxl.reader.excel import ExcelReader
    with source.open("rb") as handle:
        reader = ExcelReader(handle, read_only=True, keep_links=False)
        try:
            reader.read_manifest()
            reader.read_workbook()
            source_sheets = {rel.target: sheet.name for sheet, rel in reader.parser.find_sheets() if rel.target in worksheets}
            epoch = reader.wb.epoch
        finally:
            reader.archive.close()
    target = run_calc(source, directory)
    with checked_zip(target, ".xlsx"):
        pass
    computed = load_workbook(target, read_only=False, data_only=True, keep_links=False)
    errors = []
    formulas = calculated = 0
    try:
        with checked_zip(source, ".xlsx") as before, zipfile.ZipFile(output, "w", zipfile.ZIP_DEFLATED) as after:
            for part in before.infolist():
                current = before.read(part)
                sheet = source_sheets.get(part.filename)
                if sheet:
                    root = xml_part(current)
                    for cell in root.iter(SHEET + "c"):
                        if cell.find(SHEET + "f") is None:
                            continue
                        formulas += 1
                        address = cell.get("r")
                        result = computed[sheet][address] if address and sheet in computed.sheetnames else None
                        if result is None or result.value is None:
                            if len(errors) < 50:
                                errors.append({"sheet": sheet, "cell": address, "error": "结果为空"})
                            continue
                        put_value(cell, result, epoch)
                        calculated += 1
                        if result.data_type == "e" and len(errors) < 50:
                            errors.append({"sheet": sheet, "cell": address, "error": result.value})
                    current = etree.tostring(root, encoding="utf-8", xml_declaration=True)
                after.writestr(part, current, compress_type=zipfile.ZIP_DEFLATED)
    finally:
        computed.close()
    with checked_zip(output, ".xlsx"):
        pass
    return {"verified": True, "formulas": formulas, "calculated": calculated, "formula_errors": errors}
