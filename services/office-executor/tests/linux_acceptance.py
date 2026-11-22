"""Explicit, real Linux/gVisor acceptance. Never substitutes host execution.

Run from services/office-executor after provisioning the pinned task image:
  PYTHONDONTWRITEBYTECODE=1 python3 tests/linux_acceptance.py --allow-task-execution
"""
from __future__ import annotations

import argparse
import base64
import fcntl
import json
from pathlib import Path
import sys
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from office_executor.__main__ import environment_settings
from office_executor.contracts import file_record
from office_executor.supervisor import Supervisor
import office_executor.supervisor as supervisor_module


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
skill("bisheng-docx/scripts/render_docx.py", "output/acceptance.docx")
skill("bisheng-xlsx/scripts/inspect_workbook.py", "output/acceptance.xlsx", "--content-only")
skill("bisheng-xlsx/scripts/recalc_check.py", "output/acceptance.xlsx", "--timeout", "60")
assert load_workbook("output/acceptance.xlsx", data_only=True).active["A3"].value == 30
subprocess.run([sys.executable, "-m", "office_executor.render_workbook", "output/acceptance.xlsx"], check=True, timeout=150)
assert load_workbook("output/acceptance.xlsx", data_only=True).active["A3"].value == 30, "preview changed the original cached formula"
skill("bisheng-pptx/scripts/inspect_deck.py", "output/acceptance.pptx")
skill("bisheng-pptx/scripts/render_deck.py", "output/acceptance.pptx")
assert list(Path("scratch/preview/acceptance.docx").glob("*.png")), "DOCX render did not produce PNG"
assert list(Path("scratch/preview/acceptance.pptx").glob("*.png")), "PPTX render did not produce PNG"
for name in ("acceptance.docx", "acceptance.xlsx", "acceptance.pptx"):
    assert Path("scratch/preview", name, "acceptance.pdf").is_file(), name + " isolated PDF missing"
assert Path("scratch/preview/acceptance.xlsx/acceptance.pdf").is_file(), "XLSX PDF preview missing"
assert Path("scratch/preview/acceptance.xlsx/page-01.png").is_file(), "XLSX PNG preview missing"
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

PID_LIMIT_SCRIPT = r'''
import errno, os, signal, time
children = []
limited = False
try:
    for _ in range(128):
        try:
            pid = os.fork()
        except OSError as exc:
            assert exc.errno in (errno.EAGAIN, errno.ENOMEM)
            limited = True
            break
        if pid == 0:
            time.sleep(20)
            os._exit(0)
        children.append(pid)
    assert limited, "task PID ceiling was not enforced"
finally:
    for pid in children:
        try: os.kill(pid, signal.SIGKILL)
        except ProcessLookupError: pass
    for pid in children:
        try: os.waitpid(pid, 0)
        except ChildProcessError: pass
'''

MEMORY_LIMIT_SCRIPT = r'''
# Intentionally exceed the 256-MiB task cgroup. Either the script or the entire
# sandbox may be killed; neither result is a successful, publishable artifact.
data = bytearray(512 * 1024 * 1024)
raise AssertionError('task memory ceiling was not enforced')
'''


class AcceptanceSupervisor(Supervisor):
    """Bounded diagnostics for these fixed, non-user acceptance scripts only."""

    def _validate_result(self, request, data):
        try:
            return super()._validate_result(request, data)
        except (ValueError, OSError, RecursionError) as exc:
            report = {"acceptance_runner_validation": type(exc).__name__, "reason": str(exc)[:240]}
            if isinstance(data, dict):
                report.update(status=data.get("status"), exit_code=data.get("exit_code"),
                              duration_ms=data.get("duration_ms"),
                              stderr=str(data.get("stderr", ""))[-3000:],
                              files=[str(item.get("path", ""))[:240] for item in data.get("workspace_manifest", [])[:64]
                                     if isinstance(item, dict)])
            print(json.dumps(report, ensure_ascii=False), file=sys.stderr)
            raise


def capture_acceptance_result(*args, **kwargs):
    result = ORIGINAL_CAPTURE(*args, **kwargs)
    if result[0] != 0 and result[3] is None:
        print(json.dumps({"acceptance_container_exit": result[0],
                          "stderr": result[2][-1500:].decode("utf-8", "replace")}), file=sys.stderr)
    if result[0] == 0 and result[3] is None:
        try:
            json.loads(result[1])
        except (ValueError, RecursionError) as exc:
            # Scripts here are fixed fixtures; no arbitrary user content.
            print(json.dumps({"acceptance_runner_json": str(exc)[:240], "bytes": len(result[1]),
                              "stderr": result[2][-1000:].decode("utf-8", "replace")}), file=sys.stderr)
    return result


ORIGINAL_CAPTURE = supervisor_module._bounded_capture


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--allow-task-execution", action="store_true", required=True)
    parser.add_argument("--evidence-dir", type=Path, help="operator-selected local directory for bounded Office previews/results")
    args = parser.parse_args()
    if args.evidence_dir:
        args.evidence_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
    settings = environment_settings()
    settings.state_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
    lock = (settings.state_dir / ".supervisor.lock").open("a")
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    supervisor = AcceptanceSupervisor(settings)
    supervisor_module._bounded_capture = capture_acceptance_result
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
        # gVisor shares the host cgroup PID ceiling: exhaustion can reject the
        # fork or abort the Sentry itself. An unverified runtime outcome must
        # remain unknown (never an empty successful snapshot or an auto retry).
        ("pid_limit", PID_LIMIT_SCRIPT, ("completed", "unknown"), {"pids": 64}, []),
        ("memory_limit", MEMORY_LIMIT_SCRIPT, ("unknown", "failed"), {"memory_mb": 256, "disk_mb": 64}, []),
    ]
    report = []
    for label, script, expected, limits, inputs in tests:
        request = {"version": 1, "run_id": "acceptance_" + uuid.uuid4().hex,
                   "operation_id": "op_" + uuid.uuid4().hex, "lease_epoch": 1,
                   "script": script, "inputs": inputs, "limits": limits}
        result = supervisor.execute(request)
        if result["status"] not in (expected if isinstance(expected, tuple) else (expected,)):
            failure = {key: result.get(key) for key in ("status", "error_code", "exit_code", "duration_ms", "stderr")}
            raise AssertionError(label + " failed: " + json.dumps(failure, ensure_ascii=False)[:4000])
        report.append({"case": label, "status": result["status"], "duration_ms": result["duration_ms"],
                       "files": len(result["workspace_manifest"])})
        if label == "office_skills" and args.evidence_dir:
            # Only fixed acceptance outputs, never arbitrary model paths.
            evidence = []
            for record in result["workspace_manifest"]:
                relative = record["path"]
                if relative in {"output/acceptance.docx", "output/acceptance.xlsx", "output/acceptance.pptx"} or (
                    relative.startswith(("scratch/preview/acceptance.docx/", "scratch/preview/acceptance.pptx/", "scratch/preview/acceptance.xlsx/")) and relative.endswith(".png")
                ):
                    filename = relative.replace("/", "_")
                    destination = args.evidence_dir / filename
                    destination.write_bytes(base64.b64decode(record["content_base64"], validate=True))
                    destination.chmod(0o600)
                    evidence.append({"file": filename, "sha256": record["sha256"], "bytes": record["size"]})
            (args.evidence_dir / "manifest.json").write_text(json.dumps(evidence, ensure_ascii=False, indent=2))
            report[-1]["evidence_files"] = len(evidence)
    print(json.dumps({"runtime": "runsc", "acceptance": report}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
