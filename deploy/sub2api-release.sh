#!/usr/bin/env bash
# Immutable fork release deployment helper.
# Installed on the server as /root/sub2api-deploy/bin/sub2api-release.
set -Eeuo pipefail
umask 077

DEPLOY_DIR="${SUB2API_DEPLOY_DIR:-/root/sub2api-deploy}"
BASE_COMPOSE="${SUB2API_BASE_COMPOSE:-$DEPLOY_DIR/docker-compose.yml}"
FORK_COMPOSE="${SUB2API_FORK_COMPOSE:-$DEPLOY_DIR/docker-compose.fork.yml}"
ENV_FILE="${SUB2API_ENV_FILE:-$DEPLOY_DIR/.env}"
SERVICE="${SUB2API_SERVICE:-sub2api}"
DB_CONTAINER="${SUB2API_DB_CONTAINER:-sub2api-postgres}"
HEALTH_URL="${SUB2API_HEALTH_URL:-http://127.0.0.1:8080/health}"
RELEASE_DIR="$DEPLOY_DIR/releases"
BACKUP_DIR="$RELEASE_DIR/backups"
RECORD_DIR="$RELEASE_DIR/records"
CURRENT_RECORD="$RELEASE_DIR/current.json"
PREVIOUS_RECORD="$RELEASE_DIR/previous.json"
HISTORY_FILE="$RELEASE_DIR/history.jsonl"
LOCK_FILE="$RELEASE_DIR/deploy.lock"
HEALTH_TIMEOUT_SECONDS="${SUB2API_HEALTH_TIMEOUT_SECONDS:-120}"

log() { printf '[sub2api-release] %s\n' "$*"; }
die() { printf '[sub2api-release] ERROR: %s\n' "$*" >&2; exit 1; }

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

compose() {
  docker compose \
    --project-directory "$DEPLOY_DIR" \
    --env-file "$ENV_FILE" \
    -f "$BASE_COMPOSE" \
    -f "$FORK_COMPOSE" \
    "$@"
}

json_get() {
  local file="$1" key="$2"
  [[ -f "$file" ]] || return 1
  python3 - "$file" "$key" <<'PY'
import json, sys
with open(sys.argv[1], encoding="utf-8") as fh:
    value = json.load(fh).get(sys.argv[2], "")
if isinstance(value, bool):
    print("true" if value else "false")
elif value is not None:
    print(value)
PY
}

write_record() {
  local file="$1"
  local release_version="$2"
  local upstream_version="$3"
  local upstream_commit="$4"
  local source_commit="$5"
  local image="$6"
  local digest="$7"
  local schema_epoch="$8"
  local database_rollback="$9"
  local backup="${10}"
  local health="${11}"
  local deployed_at
  deployed_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  python3 - "$file" "$release_version" "$upstream_version" "$upstream_commit" \
    "$source_commit" "$image" "$digest" "$schema_epoch" "$database_rollback" \
    "$backup" "$health" "$deployed_at" <<'PY'
import json, os, sys
keys = [
    "release_version", "upstream_version", "upstream_commit", "source_commit",
    "image", "digest", "schema_epoch", "database_rollback", "backup", "health",
    "deployed_at",
]
record = dict(zip(keys, sys.argv[2:]))
record["schema_epoch"] = int(record["schema_epoch"]) if record["schema_epoch"] else 0
target = sys.argv[1]
tmp = target + ".tmp"
os.makedirs(os.path.dirname(target), exist_ok=True)
with open(tmp, "w", encoding="utf-8") as fh:
    json.dump(record, fh, indent=2, sort_keys=True)
    fh.write("\n")
os.replace(tmp, target)
PY
}

append_history() {
  local record="$1"
  python3 - "$record" >> "$HISTORY_FILE" <<'PY'
import json, sys
with open(sys.argv[1], encoding="utf-8") as fh:
    print(json.dumps(json.load(fh), sort_keys=True, separators=(",", ":")))
PY
}

image_label() {
  local image="$1" key="$2"
  local value
  value="$(docker image inspect --format "{{index .Config.Labels \"$key\"}}" "$image" 2>/dev/null || true)"
  [[ "$value" == "<no value>" ]] && value=""
  printf '%s' "$value"
}

container_health() {
  docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$SERVICE" 2>/dev/null || true
}

container_version() {
  docker exec "$SERVICE" /app/sub2api --version 2>&1 \
    | grep -oE '([0-9]+\.){2}[0-9]+(-ds\.[0-9]+)?' \
    | head -n 1 || true
}

snapshot_current() {
  local destination="$1"
  local image image_id repo digest version upstream epoch rollback
  image="$(docker inspect --format '{{.Config.Image}}' "$SERVICE" 2>/dev/null || true)"
  [[ -n "$image" ]] || { log "current service does not exist; no previous snapshot"; return 1; }
  image_id="$(docker inspect --format '{{.Image}}' "$SERVICE" 2>/dev/null || true)"
  repo="${image%@*}"
  digest="$(docker image inspect --format '{{join .RepoDigests " "}}' "$image" 2>/dev/null | awk '{print $1}' | sed -n 's/.*@\(sha256:[0-9a-f]*\)$/\1/p' | head -n1)"
  version="$(container_version)"
  [[ -n "$version" ]] || version="$(image_label "$image" org.opencontainers.image.version)"
  upstream="$(image_label "$image" io.baisuipingan.upstream.version)"
  [[ -n "$upstream" ]] || upstream="$version"
  epoch="$(image_label "$image" io.baisuipingan.database.schema-epoch)"
  [[ -n "$epoch" ]] || epoch="1"
  rollback="$(image_label "$image" io.baisuipingan.database.rollback)"
  [[ -n "$rollback" ]] || rollback="image-only"
  if [[ -n "$repo" && -n "$digest" ]]; then
    image="$repo@$digest"
  elif [[ -n "$image_id" ]]; then
    image="$image_id"
  fi
  write_record "$destination" "$version" "$upstream" \
    "$(image_label "$image" io.baisuipingan.upstream.commit)" \
    "$(image_label "$image" org.opencontainers.image.revision)" \
    "$image" "$digest" "$epoch" "$rollback" "" "$(container_health)"
}

preflight() {
  need_cmd docker
  need_cmd python3
  need_cmd curl
  need_cmd flock
  need_cmd gzip
  [[ -d "$DEPLOY_DIR" ]] || die "deploy directory not found: $DEPLOY_DIR"
  [[ -f "$BASE_COMPOSE" ]] || die "base compose not found: $BASE_COMPOSE"
  [[ -f "$FORK_COMPOSE" ]] || die "fork compose not found: $FORK_COMPOSE"
  [[ -f "$ENV_FILE" ]] || die "env file not found: $ENV_FILE"
  docker info >/dev/null 2>&1 || die "docker daemon unavailable"
  local config_image
  config_image="$(docker inspect --format '{{.Config.Image}}' "$SERVICE" 2>/dev/null || true)"
  [[ -n "$config_image" ]] || config_image="weishaw/sub2api:latest"
  SUB2API_IMAGE="$config_image" compose config --quiet
  [[ "$(docker inspect --format '{{.State.Running}}' "$DB_CONTAINER" 2>/dev/null || true)" == "true" ]] \
    || die "database container is not running: $DB_CONTAINER"
  local health
  health="$(container_health)"
  [[ "$health" == "healthy" ]] || die "current application is not healthy: ${health:-missing}"
  local free_kb
  free_kb="$(df -Pk "$DEPLOY_DIR" | awk 'NR==2 {print $4}')"
  (( free_kb > 5 * 1024 * 1024 )) || die "less than 5 GiB free disk space"
}

acquire_lock() {
  mkdir -p "$RELEASE_DIR" "$BACKUP_DIR" "$RECORD_DIR"
  exec 9>"$LOCK_FILE"
  flock -n 9 || die "another release operation is already running"
}

backup_database() {
  local stamp file tmp
  stamp="$(date -u +%Y%m%dT%H%M%SZ)"
  file="$BACKUP_DIR/sub2api-$stamp.dump.gz"
  tmp="$file.tmp"
  log "backing up PostgreSQL to $file" >&2
  docker exec "$DB_CONTAINER" sh -lc \
    'exec pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' \
    | gzip -c > "$tmp"
  [[ -s "$tmp" ]] || { rm -f "$tmp"; die "database backup is empty"; }
  gzip -t "$tmp" || { rm -f "$tmp"; die "database backup gzip validation failed"; }
  python3 - "$tmp" <<'PY_CHECK'
import gzip, sys
with gzip.open(sys.argv[1], "rb") as fh:
    magic = fh.read(5)
if magic != b"PGDMP":
    raise SystemExit("database backup is not a PostgreSQL custom dump")
PY_CHECK
  mv "$tmp" "$file"
  chmod 600 "$file"
  printf '%s' "$file"
}

update_env_image() {
  local image="$1" tmp
  tmp="$(mktemp "$DEPLOY_DIR/.env.release.XXXXXX")"
  TARGET_IMAGE="$image" awk '
    BEGIN { found = 0 }
    /^SUB2API_IMAGE=/ { print "SUB2API_IMAGE=" ENVIRON["TARGET_IMAGE"]; found = 1; next }
    { print }
    END { if (!found) print "SUB2API_IMAGE=" ENVIRON["TARGET_IMAGE"] }
  ' "$ENV_FILE" > "$tmp"
  chmod 600 "$tmp"
  mv "$tmp" "$ENV_FILE"
}

wait_for_health() {
  local expected_version="$1"
  local deadline=$((SECONDS + HEALTH_TIMEOUT_SECONDS))
  local status running version
  while (( SECONDS < deadline )); do
    status="$(container_health)"
    running="$(docker inspect --format '{{.State.Running}}' "$SERVICE" 2>/dev/null || true)"
    if [[ "$running" == "true" ]] && [[ "$status" == "healthy" ]]; then
      version="$(container_version)"
      if [[ -z "$expected_version" || "$version" == "$expected_version" ]]; then
        HEALTH_URL="$HEALTH_URL" curl -fsS --max-time 5 "$HEALTH_URL" >/dev/null 2>&1 \
          && return 0
      fi
    fi
    sleep 2
  done
  log "health check timed out; container status: ${status:-missing}, version: ${version:-unknown}"
  docker compose --project-directory "$DEPLOY_DIR" --env-file "$ENV_FILE" \
    -f "$BASE_COMPOSE" -f "$FORK_COMPOSE" logs --tail=120 "$SERVICE" >&2 || true
  return 1
}

switch_image() {
  local image="$1" expected_version="$2" env_backup="$3"
  local previous_env="$RELEASE_DIR/.env.before-release.$$.bak"
  cp -p "$ENV_FILE" "$previous_env"
  update_env_image "$image"
  if ! compose up -d --no-deps --force-recreate "$SERVICE"; then
    cp -p "$previous_env" "$ENV_FILE"
    rm -f "$previous_env"
    return 1
  fi
  if ! wait_for_health "$expected_version"; then
    # Keep the target image selected in .env. The caller decides whether a
    # safe image rollback is permitted from the release metadata.
    rm -f "$previous_env"
    return 1
  fi
  cp -p "$previous_env" "$env_backup"
  chmod 600 "$env_backup"
  rm -f "$previous_env"
}

restore_previous_image() {
  local previous_record="$1" expected_version image
  [[ -f "$previous_record" ]] || { log "no previous release record to restore"; return 1; }
  expected_version="$(json_get "$previous_record" release_version)"
  image="$(json_get "$previous_record" image)"
  [[ -n "$image" ]] || { log "previous image reference is empty"; return 1; }
  log "restoring previous image: $image ($expected_version)"
  update_env_image "$image"
  compose up -d --no-deps --force-recreate "$SERVICE"
  wait_for_health "$expected_version"
}

deploy_image() {
  local image="$1"
  local release_version upstream_version upstream_commit source_commit schema_epoch database_rollback
  local digest repo image_id backup env_backup health

  [[ "$image" =~ ^[^[:space:]]+@sha256:[0-9a-f]{64}$ ]] \
    || die "deploy requires an immutable image@sha256 digest reference"
  preflight
  acquire_lock

  log "pulling $image"
  docker pull "$image" >/dev/null

  release_version="$(image_label "$image" org.opencontainers.image.version)"
  upstream_version="$(image_label "$image" io.baisuipingan.upstream.version)"
  upstream_commit="$(image_label "$image" io.baisuipingan.upstream.commit)"
  source_commit="$(image_label "$image" org.opencontainers.image.revision)"
  schema_epoch="$(image_label "$image" io.baisuipingan.database.schema-epoch)"
  database_rollback="$(image_label "$image" io.baisuipingan.database.rollback)"
  [[ -n "$schema_epoch" ]] || schema_epoch="1"
  [[ -n "$database_rollback" ]] || database_rollback="backup-required"
  [[ "$database_rollback" =~ ^(image-only|backup-required)$ ]] \
    || die "invalid database rollback label: $database_rollback"
  [[ -n "$release_version" ]] || die "image is missing org.opencontainers.image.version"
  [[ "$release_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+-ds\.[1-9][0-9]*$ ]] \
    || die "invalid fork release version label: $release_version"
  [[ "$upstream_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] \
    || die "invalid upstream version label: $upstream_version"
  [[ "$(image_label "$image" io.baisuipingan.build.type)" == "fork" ]] \
    || die "refusing to deploy an image that is not a fork build"
  [[ -n "$upstream_version" ]] || die "image is missing io.baisuipingan.upstream.version"
  digest="${image##*@}"
  repo="${image%@*}"
  image_id="$(docker image inspect --format '{{.Id}}' "$image")"

  [[ -f "$CURRENT_RECORD" ]] || snapshot_current "$PREVIOUS_RECORD" || true
  if [[ -f "$CURRENT_RECORD" ]]; then
    cp "$CURRENT_RECORD" "$PREVIOUS_RECORD"
  fi

  backup="$(backup_database)"
  env_backup="$RELEASE_DIR/env/sub2api.env.$(date -u +%Y%m%dT%H%M%SZ).bak"
  mkdir -p "$RELEASE_DIR/env"

  if ! switch_image "$image" "$release_version" "$env_backup"; then
    log "new image failed to become healthy"
    local previous_epoch current_epoch previous_rollback
    previous_epoch="$(json_get "$PREVIOUS_RECORD" schema_epoch 2>/dev/null || echo 0)"
    previous_rollback="$(json_get "$PREVIOUS_RECORD" database_rollback 2>/dev/null || echo backup-required)"
    if [[ "$database_rollback" == "image-only" && "$schema_epoch" == "$previous_epoch" ]]; then
      log "database manifest permits image rollback; restoring previous release"
      if restore_previous_image "$PREVIOUS_RECORD"; then
        die "deployment failed; previous image restored from $previous_epoch-compatible baseline"
      fi
      die "deployment failed and automatic previous-image restore also failed"
    fi
    die "deployment failed; automatic image rollback refused (database_rollback=$database_rollback, previous_epoch=$previous_epoch, target_epoch=$schema_epoch). Manual intervention required."
  fi

  health="$(container_health)"
  write_record "$CURRENT_RECORD" "$release_version" "$upstream_version" \
    "$upstream_commit" "$source_commit" "$repo@$digest" "$digest" \
    "$schema_epoch" "$database_rollback" "$backup" "$health"
  cp "$CURRENT_RECORD" "$RECORD_DIR/$release_version.json"
  append_history "$CURRENT_RECORD"
  log "deployed $release_version with image id $image_id"
  log "backup: $backup"
  log "previous release preserved in $PREVIOUS_RECORD"
  cleanup_retention
}

cleanup_retention() {
  local backup record ref
  while IFS= read -r backup; do
    [[ -n "$backup" ]] || continue
    rm -f -- "$backup"
    log "removed old database backup: $backup"
  done < <(
    find "$BACKUP_DIR" -maxdepth 1 -type f -name '*.dump.gz' -printf '%T@ %p\n' 2>/dev/null       | sort -nr | awk 'NR > 3 {sub(/^[^ ]+ /, ""); print}'
  )

  while IFS= read -r record; do
    [[ -n "$record" ]] || continue
    rm -f -- "$record"
    log "removed old release record: $record"
  done < <(
    find "$RECORD_DIR" -maxdepth 1 -type f -name '*.json' -printf '%T@ %p\n' 2>/dev/null       | sort -nr | awk 'NR > 3 {sub(/^[^ ]+ /, ""); print}'
  )

  [[ -f "$HISTORY_FILE" ]] || return 0
  while IFS= read -r ref; do
    [[ -n "$ref" ]] || continue
    docker image rm "$ref" >/dev/null 2>&1 || true
    log "pruned old local image if unused: $ref"
  done < <(
    python3 - "$HISTORY_FILE" <<'PY_RETENTION'
import json, sys
seen = []
with open(sys.argv[1], encoding="utf-8") as fh:
    for line in reversed(fh.readlines()):
        line = line.strip()
        if not line:
            continue
        try:
            image = json.loads(line).get("image", "")
        except json.JSONDecodeError:
            continue
        if image and image not in seen:
            seen.append(image)
for image in seen[3:]:
    print(image)
PY_RETENTION
  )
}

rollback_release() {
  local target_record explicit_release="${1:-}"
  preflight
  acquire_lock
  if [[ -n "$explicit_release" ]]; then
    target_record="$RECORD_DIR/$explicit_release.json"
    [[ -f "$target_record" ]] || die "release record not found: $target_record"
  else
    target_record="$PREVIOUS_RECORD"
    [[ -f "$target_record" ]] || die "no previous release record found"
  fi
  [[ -f "$CURRENT_RECORD" ]] || die "current release record not found"

  local current_epoch target_epoch current_policy target_image target_version backup
  local old_current old_backup
  current_epoch="$(json_get "$CURRENT_RECORD" schema_epoch)"
  target_epoch="$(json_get "$target_record" schema_epoch)"
  current_policy="$(json_get "$CURRENT_RECORD" database_rollback)"
  if [[ "$current_epoch" != "$target_epoch" ]]; then
    die "schema epoch differs ($current_epoch -> $target_epoch); restore the pre-deploy database backup before switching images"
  fi
  if [[ "$current_policy" != "image-only" ]]; then
    die "current release is marked $current_policy; image-only rollback is not allowed"
  fi

  target_image="$(json_get "$target_record" image)"
  target_version="$(json_get "$target_record" release_version)"
  [[ -n "$target_image" ]] || die "target image reference is empty"
  [[ "$target_version" != "$(json_get "$CURRENT_RECORD" release_version)" ]] \
    || die "target release is already current: $target_version"

  docker pull "$target_image" >/dev/null
  backup="$(backup_database)"
  old_current="$(mktemp "$RELEASE_DIR/current.before-rollback.XXXXXX.json")"
  cp "$CURRENT_RECORD" "$old_current"
  if ! switch_image "$target_image" "$target_version" "$RELEASE_DIR/env/sub2api.env.rollback.$(date -u +%Y%m%dT%H%M%SZ).bak"; then
    log "rollback image failed health checks; restoring the release that was current"
    local interrupted_version interrupted_image
    interrupted_version="$(json_get "$old_current" release_version)"
    interrupted_image="$(json_get "$old_current" image)"
    if [[ -n "$interrupted_image" ]]; then
      update_env_image "$interrupted_image"
      compose up -d --no-deps --force-recreate "$SERVICE" || true
      wait_for_health "$interrupted_version" || true
    fi
    cp "$old_current" "$CURRENT_RECORD"
    rm -f "$old_current"
    die "rollback failed; current release was restored when possible"
  fi

  cp "$old_current" "$PREVIOUS_RECORD"
  old_backup="$(json_get "$target_record" backup)"
  [[ -n "$old_backup" ]] || old_backup="$backup"
  write_record "$CURRENT_RECORD" "$target_version" \
    "$(json_get "$target_record" upstream_version)" \
    "$(json_get "$target_record" upstream_commit)" \
    "$(json_get "$target_record" source_commit)" \
    "$target_image" "$(json_get "$target_record" digest)" \
    "$target_epoch" "$(json_get "$target_record" database_rollback)" \
    "$old_backup" "$(container_health)"
  cp "$CURRENT_RECORD" "$RECORD_DIR/$target_version.json"
  append_history "$CURRENT_RECORD"
  rm -f "$old_current"
  log "rolled back to $target_version"
  log "rollback backup: $backup"
  cleanup_retention
}

show_status() {
  preflight 2>/dev/null || true
  log "deploy directory: $DEPLOY_DIR"
  local current_image
  current_image="$(docker inspect --format '{{.Config.Image}}' "$SERVICE" 2>/dev/null || true)"
  [[ -n "$current_image" ]] || current_image="weishaw/sub2api:latest"
  SUB2API_IMAGE="$current_image" docker compose --project-directory "$DEPLOY_DIR" --env-file "$ENV_FILE" \
    -f "$BASE_COMPOSE" -f "$FORK_COMPOSE" ps 2>/dev/null || true
  log "compose image: ${current_image:-missing}"
  log "running version: $(container_version)"
  log "health: $(container_health)"
  if [[ -f "$CURRENT_RECORD" ]]; then
    log "release record: $(cat "$CURRENT_RECORD")"
  else
    log "release record: not created yet"
  fi
  if [[ -f "$PREVIOUS_RECORD" ]]; then
    log "previous record: $(cat "$PREVIOUS_RECORD")"
  fi
}

show_backups() {
  mkdir -p "$BACKUP_DIR"
  find "$BACKUP_DIR" -maxdepth 1 -type f -name '*.dump.gz' -printf '%TY-%Tm-%Td %TH:%TM:%TS %10s %p\n' 2>/dev/null \
    | sort -r || ls -lh "$BACKUP_DIR"
}

usage() {
  cat <<'USAGE'
Usage:
  sub2api-release status
  sub2api-release deploy <registry/repository@sha256:digest>
  sub2api-release rollback [--release <release-version>]
  sub2api-release rollback --digest <sha256:digest>  # deploy a verified saved digest
  sub2api-release backups
USAGE
}

main() {
  local command="${1:-}"
  case "$command" in
    status) show_status ;;
    deploy)
      [[ $# -eq 2 ]] || { usage; exit 2; }
      deploy_image "$2"
      ;;
    rollback)
      local release=""
      shift || true
      if [[ $# -eq 0 ]]; then
        rollback_release
      elif [[ $# -eq 2 && "$1" == "--release" ]]; then
        release="$2"
        rollback_release "$release"
      elif [[ $# -eq 2 && "$1" == "--digest" ]]; then
        [[ "$2" =~ ^sha256:[0-9a-f]{64}$ ]] || die "invalid digest: $2"
        deploy_image "ghcr.io/baisuipingan/sub2api@$2"
      else
        usage
        exit 2
      fi
      ;;
    backups) show_backups ;;
    *) usage; exit 2 ;;
  esac
}

main "$@"
