#!/usr/bin/env bash
# Exercise the epoch-2 release policy with fake Docker and an isolated deploy
# directory. Never connects to a daemon or production host.
set -Eeuo pipefail

source_root="$(cd "$(dirname "$0")/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

cat > "$tmp/harness.sh" <<'HARNESS'
#!/usr/bin/env bash
set -Eeuo pipefail
export SUB2API_DEPLOY_DIR="$TEST_DIR"
source "$TEST_SOURCE_ROOT/deploy/sub2api-release.sh"
mkdir -p "$BACKUP_DIR" "$RECORD_DIR"
cat > "$CURRENT_RECORD" <<'JSON'
{"release_version":"0.2.20-ds.1","schema_epoch":1,"database_rollback":"image-only"}
JSON

preflight() { :; }
acquire_lock() { :; }
docker() {
  case "$*" in
    'image inspect '*) printf 'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc\n' ;;
    *) : ;;
  esac
}
image_label() {
  case "$2" in
    org.opencontainers.image.version) printf '0.2.20-ds.2' ;;
    io.baisuipingan.upstream.version) printf '0.2.20' ;;
    io.baisuipingan.upstream.commit) printf 'upstream-commit' ;;
    org.opencontainers.image.revision) printf 'source-commit' ;;
    io.baisuipingan.database.schema-epoch) printf '2' ;;
    io.baisuipingan.database.rollback) printf 'backup-required' ;;
    io.baisuipingan.build.type) printf 'fork' ;;
    *) : ;;
  esac
}
backup_database() {
  if [[ "$TEST_CASE" == backup_failure ]]; then
    return 7
  fi
  printf 'validated PostgreSQL dump\n' > "$BACKUP_DIR/pre-deploy.dump.gz"
  printf '%s' "$BACKUP_DIR/pre-deploy.dump.gz"
}
switch_image() {
  [[ -s "$BACKUP_DIR/pre-deploy.dump.gz" ]] || return 8
  printf 'attempted\n' > "$TEST_DIR/switch.marker"
  [[ "$TEST_CASE" != switch_failure ]]
}
restore_previous_image() {
  printf 'unsafe rollback\n' > "$TEST_DIR/restore.marker"
}
container_health() { printf 'healthy'; }
cleanup_retention() { :; }

if [[ "$TEST_CASE" == rollback_refusal ]]; then
  cat > "$CURRENT_RECORD" <<'JSON'
{"release_version":"0.2.20-ds.2","schema_epoch":2,"database_rollback":"backup-required"}
JSON
  cat > "$PREVIOUS_RECORD" <<'JSON'
{"release_version":"0.2.20-ds.1","schema_epoch":2,"database_rollback":"image-only"}
JSON
  rollback_release
else
  deploy_image "ghcr.io/test/sub2api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
fi
HARNESS

run_case() {
  local name="$1"
  mkdir -p "$tmp/$name"
  TEST_SOURCE_ROOT="$source_root" TEST_DIR="$tmp/$name" TEST_CASE="$name" \
    bash "$tmp/harness.sh" > "$tmp/$name.log" 2>&1
}

run_case success
[[ -s "$tmp/success/releases/backups/pre-deploy.dump.gz" ]]
[[ -f "$tmp/success/switch.marker" ]]
python3 - "$tmp/success/releases/current.json" <<'PY'
import json, sys
with open(sys.argv[1], encoding='utf-8') as fh:
    record = json.load(fh)
assert record['schema_epoch'] == 2
assert record['database_rollback'] == 'backup-required'
assert record['backup'].endswith('/pre-deploy.dump.gz')
PY

if run_case backup_failure; then
  echo 'deployment proceeded without a PostgreSQL backup' >&2
  exit 1
fi
[[ ! -f "$tmp/backup_failure/switch.marker" ]]

if run_case switch_failure; then
  echo 'failed epoch-2 deployment unexpectedly succeeded' >&2
  exit 1
fi
[[ -s "$tmp/switch_failure/releases/backups/pre-deploy.dump.gz" ]]
[[ -f "$tmp/switch_failure/switch.marker" ]]
[[ ! -f "$tmp/switch_failure/restore.marker" ]]
grep -F 'automatic image rollback refused' "$tmp/switch_failure.log" >/dev/null

if run_case rollback_refusal; then
  echo 'backup-required release allowed image-only rollback' >&2
  exit 1
fi
[[ ! -f "$tmp/rollback_refusal/switch.marker" ]]
grep -F 'image-only rollback is not allowed' "$tmp/rollback_refusal.log" >/dev/null

printf 'sub2api-release epoch-2 backup policy tests passed\n'
