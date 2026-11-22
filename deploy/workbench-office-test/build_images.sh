#!/bin/sh
# Build the test deployment's document service on the Linux host.
set -eu
cd /opt/milo-workbench-test
test -f secrets/compose.env
set -a
. ./secrets/compose.env
set +a
mkdir -p logs
docker build --build-arg PYTHON_BASE_IMAGE="$PYTHON_BASE_IMAGE" \
  -t milo-office-tools-test:latest source/services/workbench-office-tools \
  >logs/tools-build.log 2>&1
python3 - <<'PY'
from pathlib import Path
import subprocess
file = Path('/opt/milo-workbench-test/secrets/compose.env')
values = dict(line.split('=', 1) for line in file.read_text().splitlines() if line and not line.startswith('#'))
values['OFFICE_TOOLS_IMAGE'] = subprocess.check_output(
    ['docker', 'image', 'inspect', '--format', '{{.Id}}', 'milo-office-tools-test:latest'], text=True
).strip()
temp = file.with_suffix('.tmp')
temp.touch(mode=0o600, exist_ok=False)
temp.write_text(''.join(key+'='+values[key]+'\n' for key in sorted(values)))
temp.replace(file)
print('document service test image built and pinned')
PY
