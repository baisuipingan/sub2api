#!/usr/bin/env bash
# Regression test for rollback record handling without touching Docker or a
# real deploy directory. The script is sourced so its filesystem helpers can
# be exercised with deterministic fake operations.
set -Eeuo pipefail

source_root="$(cd "$(dirname "$0")/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

deploy_dir="$tmp/deploy"
mkdir -p "$deploy_dir/releases/records" "$deploy_dir/releases/backups"
printf 'fixture\n' > "$deploy_dir/.env"
printf 'services: {}\n' > "$deploy_dir/docker-compose.yml"
printf 'services: {}\n' > "$deploy_dir/docker-compose.fork.yml"

export SUB2API_DEPLOY_DIR="$deploy_dir"
# shellcheck source=../../deploy/sub2api-release.sh
source "$source_root/deploy/sub2api-release.sh"

fork_digest="sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
official_digest="sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
cat > "$CURRENT_RECORD" <<JSON
{
  "release_version": "0.2.17-ds.1",
  "upstream_version": "0.2.17",
  "upstream_commit": "fork-upstream-commit",
  "source_commit": "fork-source-commit",
  "image": "ghcr.io/test/sub2api@$fork_digest",
  "digest": "$fork_digest",
  "schema_epoch": 1,
  "database_rollback": "image-only",
  "backup": "/old/fork-backup.dump.gz",
  "health": "healthy",
  "deployed_at": "2026-10-19T00:00:00Z"
}
JSON
cat > "$PREVIOUS_RECORD" <<JSON
{
  "release_version": "0.2.17",
  "upstream_version": "0.2.17",
  "upstream_commit": "official-upstream-commit",
  "source_commit": "official-source-commit",
  "image": "weishaw/sub2api@$official_digest",
  "digest": "$official_digest",
  "schema_epoch": 1,
  "database_rollback": "image-only",
  "backup": "/old/official-backup.dump.gz",
  "health": "healthy",
  "deployed_at": "2026-10-18T00:00:00Z"
}
JSON

docker() { return 0; }
preflight() { :; }
acquire_lock() { mkdir -p "$RELEASE_DIR" "$BACKUP_DIR" "$RECORD_DIR"; }
backup_database() { printf '%s' "$BACKUP_DIR/pre-rollback.dump.gz"; }
switch_image() { return 0; }
container_health() { printf 'healthy'; }
cleanup_retention() { :; }

rollback_release

python3 - "$CURRENT_RECORD" "$PREVIOUS_RECORD" "$official_digest" <<'PY'
import json
import sys

current_path, previous_path, official_digest = sys.argv[1:]
with open(current_path, encoding="utf-8") as fh:
    current = json.load(fh)
with open(previous_path, encoding="utf-8") as fh:
    previous = json.load(fh)

assert current["image"] == f"weishaw/sub2api@{official_digest}"
assert current["digest"] == official_digest
assert current["upstream_commit"] == "official-upstream-commit"
assert current["source_commit"] == "official-source-commit"
assert current["backup"].endswith("/pre-rollback.dump.gz")
assert previous["release_version"] == "0.2.17-ds.1"
assert previous["upstream_commit"] == "fork-upstream-commit"
assert previous["source_commit"] == "fork-source-commit"
PY

printf 'sub2api-release rollback metadata tests passed\n'
