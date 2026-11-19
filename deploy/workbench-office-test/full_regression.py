"""Whole-backend regression in one bounded tool container; no app restart."""
from pathlib import Path
import subprocess
import sys
import shutil
import tempfile

root = Path("/opt/milo-workbench-test")
environment = dict(line.split("=", 1) for line in (root / "secrets/compose.env").read_text().splitlines() if "=" in line)
commands = [
    ("all-tests", ["go", "test", "./...", "-count=1", "-p=1", "-timeout=6m"]),
    ("all-unit-tests", ["go", "test", "-tags", "unit", "./...", "-count=1", "-p=1", "-timeout=6m"]),
    ("golangci-lint", ["/tools/golangci-lint", "run", "--timeout=20m", "--concurrency=2", "./..."]),
]
# Ent's schema contract test creates .entc beside its sources. Give tests their
# own writable copy while keeping the deployed source and live services intact.
scratch = tempfile.TemporaryDirectory(prefix="go-regression-", dir=root / "app-build")
backend_copy = Path(scratch.name) / "backend"
shutil.copytree(root / "source/backend", backend_copy, ignore=shutil.ignore_patterns(".entc"))
results = []
for label, command in commands:
    arguments = [
        "docker", "run", "--rm", "--name", "milo-office-full-regression",
        "--memory", "2g", "--cpus", "2", "--network", "host",
        "-v", str(root / "source") + ":/repo:ro",
        "-v", str(backend_copy) + ":/repo/backend",
        "-v", str(root / "tools") + ":/tools:ro",
        "-v", "milo-workbench-gomod:/go/pkg/mod",
        "-v", "milo-workbench-gobuild:/root/.cache/go-build",
        "-w", "/repo/backend", "-e", "GOTOOLCHAIN=local", "-e", "GOMAXPROCS=2",
        "-e", "GOMEMLIMIT=1300MiB", "-e", "CGO_ENABLED=0",
        environment["GO_IMAGE"], *command,
    ]
    path = root / "logs" / ("final-" + label + ".log")
    with path.open("w") as output:
        result = subprocess.run(arguments, stdout=output, stderr=subprocess.STDOUT)
    results.append((label, result.returncode))
    print(label, "exit", result.returncode, flush=True)
    if result.returncode:
        print(path.read_text()[-14000:], flush=True)
scratch.cleanup()
print("whole backend regression:", results, flush=True)
sys.exit(1 if any(status for _, status in results) else 0)
