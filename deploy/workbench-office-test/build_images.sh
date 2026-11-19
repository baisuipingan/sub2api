#!/bin/sh
# Explicit acceptance-only image build, on the Linux compute host.
set -eu
cd /opt/milo-workbench-test
test -f secrets/compose.env
set -a
. ./secrets/compose.env
set +a
mkdir -p logs
docker build --build-arg PYTHON_BASE_IMAGE="$PYTHON_BASE_IMAGE" \
  -f source/services/office-executor/Dockerfile -t milo-office-task-test:latest source \
  >logs/task-build.log 2>&1
docker build --build-arg PYTHON_BASE_IMAGE="$PYTHON_BASE_IMAGE" --build-arg DOCKER_CLI_IMAGE="$DOCKER_CLI_IMAGE" \
  -f source/services/office-executor/Dockerfile.supervisor -t milo-office-supervisor-test:latest source \
  >logs/supervisor-build.log 2>&1
docker build --build-arg PYTHON_BASE_IMAGE="$PYTHON_BASE_IMAGE" \
  -t milo-office-worker-test:latest source/services/office-agent \
  >logs/worker-build.log 2>&1
python3 - <<'PY'
from pathlib import Path
import subprocess
file = Path('/opt/milo-workbench-test/secrets/compose.env')
values = dict(line.split('=', 1) for line in file.read_text().splitlines() if line and not line.startswith('#'))
for key, image in {'TASK_IMAGE':'milo-office-task-test:latest', 'SUPERVISOR_IMAGE':'milo-office-supervisor-test:latest', 'WORKER_IMAGE':'milo-office-worker-test:latest'}.items():
    values[key] = subprocess.check_output(['docker','image','inspect','--format','{{.Id}}',image], text=True).strip()
file.write_text(''.join(key+'='+values[key]+'\n' for key in sorted(values)))
file.chmod(0o600)
print('task, supervisor and worker test images built and pinned; credentials not printed')
PY
