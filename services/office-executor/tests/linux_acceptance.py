"""Explicit, real Linux/gVisor acceptance. Never substitutes host execution.

Run from services/office-executor after provisioning the pinned task image:
  PYTHONDONTWRITEBYTECODE=1 python3 tests/linux_acceptance.py --allow-task-execution
"""
from __future__ import annotations

import argparse
import fcntl
import json
from pathlib import Path
import sys
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from office_executor.__main__ import environment_settings
from office_executor.contracts import file_record
from office_executor.supervisor import Supervisor


OFFICE_SCRIPT = r'''
from pathlib import Path
import subprocess, sys
from docx import Document
from openpyxl import Workbook, load_workbook
from pptx import Presentation

doc = Document()
doc.add_heading("Milo 办公执行验收", 0)
doc.add_paragraph("此文档必须由真实 Bisheng 技能检查并渲染，不能只检查文件存在。")
doc.save("output/acceptance.docx")
book = Workbook()
sheet = book.active
sheet["A1"] = 10
sheet["A2"] = 20
sheet["A3"] = "=SUM(A1:A2)"
book.save("output/acceptance.xlsx")
deck = Presentation()
slide = deck.slides.add_slide(deck.slide_layouts[0])
slide.shapes.title.text = "Milo 办公验收"
slide.placeholders[1].text = "真实技能、独立渲染、文件重读"
deck.save("output/acceptance.pptx")

def skill(relative, *args):
    subprocess.run([sys.executable, "skills/" + relative, *args], check=True, timeout=90)

skill("bisheng-docx/scripts/inspect_docx.py", "output/acceptance.docx", "--content-only")
skill("bisheng-docx/scripts/render_docx.py", "output/acceptance.docx", "--outdir", "scratch/preview/docx")
skill("bisheng-xlsx/scripts/inspect_workbook.py", "output/acceptance.xlsx", "--content-only")
skill("bisheng-xlsx/scripts/recalc_check.py", "output/acceptance.xlsx", "--timeout", "60")
assert load_workbook("output/acceptance.xlsx", data_only=True).active["A3"].value == 30
skill("bisheng-pptx/scripts/inspect_deck.py", "output/acceptance.pptx")
skill("bisheng-pptx/scripts/render_deck.py", "output/acceptance.pptx", "--outdir", "scratch/preview/pptx")
assert list(Path("scratch/preview/docx").glob("*.png")), "DOCX render did not produce PNG"
assert list(Path("scratch/preview/pptx").glob("*.png")), "PPTX render did not produce PNG"
print("official_skill_office_acceptance_ok")
'''

ISOLATION_SCRIPT = r'''
from pathlib import Path
import os, socket
assert os.getuid() == 65532
assert not Path("/var/run/docker.sock").exists()
assert not any(k for k in os.environ if any(s in k.upper() for s in ("TOKEN", "SECRET", "API_KEY", "AWS_")))
assert Path("uploads/input.txt").read_text() == "private input"
for forbidden in ("/etc/executor-write-probe", "uploads/input.txt", "skills/write-probe"):
    try:
        Path(forbidden).write_text("must fail")
    except OSError:
        pass
    else:
        raise AssertionError("write allowed outside writable zones: " + forbidden)
with socket.socket() as connection:
    connection.settimeout(2)
    try:
        connection.connect(("1.1.1.1", 443))
    except OSError:
        pass
    else:
        raise AssertionError("network unexpectedly available")
Path("output/proof.txt").write_text("isolated")
'''


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--allow-task-execution", action="store_true", required=True)
    parser.parse_args()
    settings = environment_settings()
    settings.state_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
    lock = (settings.state_dir / ".supervisor.lock").open("a")
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    supervisor = Supervisor(settings)
    supervisor.doctor()  # Fail before any script if not genuine Linux/gVisor.
    supervisor.recover()
    # This acceptance tool should run with the service stopped, not share its journal.
    tests = [
        ("isolation", ISOLATION_SCRIPT, "completed", {}, [file_record("uploads/input.txt", b"private input")]),
        ("office_skills", OFFICE_SCRIPT, "completed", {}, []),
        ("reject_symlink", "from pathlib import Path; Path('output/leak.txt').symlink_to('/etc/passwd')", "invalid_output", {}, []),
        ("timeout", "import time; time.sleep(10)", "timed_out", {"timeout_seconds": 1}, []),
        ("output_limit", "from pathlib import Path; Path('output/large.txt').write_text('x'*100)", "failed", {"max_output_bytes": 8}, []),
        ("disk_limit", "from pathlib import Path\ntry:\n Path('scratch/.tmp/fill').write_bytes(b'x'*(24*1024*1024))\nexcept OSError as exc:\n assert exc.errno==28\nelse:\n raise AssertionError('tmpfs size not enforced')", "completed", {"disk_mb": 16}, []),
    ]
    report = []
    for label, script, expected, limits, inputs in tests:
        request = {"version": 1, "run_id": "acceptance_" + uuid.uuid4().hex,
                   "operation_id": "op_" + uuid.uuid4().hex, "lease_epoch": 1,
                   "script": script, "inputs": inputs, "limits": limits}
        result = supervisor.execute(request)
        if result["status"] != expected:
            raise AssertionError(label + " failed: " + json.dumps(result, ensure_ascii=False)[:4000])
        report.append({"case": label, "status": result["status"], "duration_ms": result["duration_ms"],
                       "files": len(result["workspace_manifest"])})
    print(json.dumps({"runtime": "runsc", "acceptance": report}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
