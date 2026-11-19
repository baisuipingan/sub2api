#!/bin/sh
set -eu
cd /opt/milo-workbench-test
set -a
. ./secrets/compose.env
set +a
python3 - <<'PY'
import subprocess
names = subprocess.check_output(['docker','ps','--format','{{.Names}}'], text=True)
if any('go-check' in name or 'integration' in name for name in names.splitlines()):
    raise SystemExit('Go test container is active; run acceptance after it exits')
PY
mkdir -p executor-acceptance-state logs/office-evidence
chmod 700 executor-acceptance-state logs/office-evidence
# The supervisor owns a different journal from the running service. The one
# socket is visible only to this trusted acceptance driver, never task code.
docker run --rm --name milo-office-executor-acceptance \
  --read-only --cap-drop=ALL --security-opt=no-new-privileges:true \
  --memory=768m --cpus=1 --pids-limit=128 \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=16m \
  --network none \
  -e OFFICE_EXECUTOR_IMAGE="$TASK_IMAGE" \
  -e OFFICE_EXECUTOR_STATE_DIR=/opt/milo-workbench-test/executor-acceptance-state \
  -e OFFICE_EXECUTOR_CONCURRENCY=1 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /opt/milo-workbench-test/executor-acceptance-state:/opt/milo-workbench-test/executor-acceptance-state \
  -v /opt/milo-workbench-test/logs/office-evidence:/evidence \
  -v /opt/milo-workbench-test/source/services/office-executor/tests:/tests:ro \
  --entrypoint python "$SUPERVISOR_IMAGE" /tests/linux_acceptance.py \
  --allow-task-execution --evidence-dir /evidence >logs/executor-acceptance.json 2>logs/executor-acceptance.err
echo 'real gVisor/Office acceptance passed; artifacts and previews in logs/office-evidence'
