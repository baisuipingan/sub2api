#!/usr/bin/env bash
# Prepare and publish immutable Sub2API fork releases.
#
# Official tags only describe the upstream baseline. Fork tags intentionally use
# the separate `fork-v<official>-<sequence>` namespace so the upstream release
# workflow is never triggered.
set -Eeuo pipefail

ROOT_DIR="$(git rev-parse --show-toplevel 2>/dev/null)" || {
  printf 'fork-release: run this command inside the repository\n' >&2
  exit 1
}
cd "$ROOT_DIR"

UPSTREAM_REMOTE="${UPSTREAM_REMOTE:-upstream}"
UPSTREAM_REPO="${UPSTREAM_REPO:-Wei-Shaw/sub2api}"
ORIGIN_REMOTE="${ORIGIN_REMOTE:-origin}"
FORK_VERSION_FILE="backend/cmd/server/VERSION"
GITHUB_RESOLVE_IP="${GITHUB_RESOLVE_IP:-}"

GIT_NET_OPTS=(-c http.version=HTTP/1.1)
if [[ -n "$GITHUB_RESOLVE_IP" ]]; then
  GIT_NET_OPTS+=(-c "http.curloptResolve=github.com:443:${GITHUB_RESOLVE_IP}")
fi

log() { printf '[fork-release] %s\n' "$*"; }
die() { printf '[fork-release] ERROR: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'USAGE_EOF'
Usage:
  ./scripts/fork-release.sh status
  ./scripts/fork-release.sh prepare <official-tag>
  ./scripts/fork-release.sh publish <official-tag> [--push]

Examples:
  ./scripts/fork-release.sh status
  ./scripts/fork-release.sh prepare v0.2.18
  ./scripts/fork-release.sh publish v0.2.18
  ./scripts/fork-release.sh publish v0.2.18 --push

prepare creates sync/<official-tag> and merges the official tag with --no-ff.
publish creates fork-v<official-tag>-<sequence> locally. Only --push writes to
origin. Resolve merge conflicts manually; this command never resolves them.
USAGE_EOF
}

normalize_official_tag() {
  local raw="${1:-}" version
  version="${raw#v}"
  [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] \
    || die "official tag must look like v0.2.18 (got: ${raw:-<empty>})"
  printf 'v%s' "$version"
}

require_clean_worktree() {
  if [[ -n "$(git status --porcelain)" ]]; then
    git status --short >&2
    die "working tree must be clean (including staged and untracked files)"
  fi
}

fetch_upstream() {
  local allow_cached="${1:-false}"
  git remote get-url "$UPSTREAM_REMOTE" >/dev/null 2>&1 \
    || die "missing upstream remote '$UPSTREAM_REMOTE'; add https://github.com/${UPSTREAM_REPO}.git"
  local -a curl_opts=(--connect-timeout 5 --max-time 10)
  [[ -n "$GITHUB_RESOLVE_IP" ]] && curl_opts+=(--resolve "github.com:443:${GITHUB_RESOLVE_IP}")
  if [[ "$allow_cached" == true ]] && command -v curl >/dev/null 2>&1 \
    && ! curl -fsSI "${curl_opts[@]}" https://github.com >/dev/null 2>&1 \
    && git rev-parse -q --verify "refs/remotes/$UPSTREAM_REMOTE/main" >/dev/null; then
    log "warning: GitHub is unreachable; using cached $UPSTREAM_REMOTE/main" >&2
    return 0
  fi
  log "fetching official tags and commits from $UPSTREAM_REMOTE"
  local attempt
  for attempt in 1 2; do
    if git "${GIT_NET_OPTS[@]}" fetch --prune --tags --force "$UPSTREAM_REMOTE"; then
      return 0
    fi
    [[ "$attempt" -eq 2 ]] || { log "fetch failed (attempt $attempt/2); retrying"; sleep 1; }
  done
  die "failed to fetch $UPSTREAM_REMOTE after 2 attempts"
}

fetch_origin_tags() {
  git remote get-url "$ORIGIN_REMOTE" >/dev/null 2>&1 || return 0
  local attempt
  for attempt in 1 2 3; do
    if git "${GIT_NET_OPTS[@]}" fetch --tags --force "$ORIGIN_REMOTE"; then
      return 0
    fi
    [[ "$attempt" -eq 3 ]] || { log "origin tag fetch failed (attempt $attempt/3); retrying"; sleep "$attempt"; }
  done
  die "failed to fetch tags from $ORIGIN_REMOTE after 3 attempts"
}

require_official_tag() {
  local tag="$1"
  git rev-parse -q --verify "refs/tags/$tag^{commit}" >/dev/null \
    || die "official tag does not exist after fetch: $tag"
}

current_official_baseline() {
  git describe --tags --abbrev=0 --match 'v[0-9]*' HEAD 2>/dev/null || printf 'none'
}

current_fork_release() {
  git describe --tags --abbrev=0 --match 'fork-v*' HEAD 2>/dev/null || printf 'none'
}

version_cmp() {
  python3 - "$1" "$2" <<'PY_EOF'
import sys

def key(value):
    value = value.strip().lstrip("v").split("-", 1)[0].split("+", 1)[0]
    parts = [int(x) for x in value.split(".")[:3]]
    return tuple((parts + [0, 0, 0])[:3])

a, b = key(sys.argv[1]), key(sys.argv[2])
print(-1 if a < b else 1 if a > b else 0)
PY_EOF
}

latest_official_release() {
  local response
  if [[ -n "${GH_TOKEN:-}" ]]; then
    response="$(curl -fsSL --max-time 20 \
      -H "Authorization: Bearer $GH_TOKEN" \
      -H 'Accept: application/vnd.github+json' \
      -H 'X-GitHub-Api-Version: 2022-11-28' \
      "https://api.github.com/repos/${UPSTREAM_REPO}/releases/latest" 2>/dev/null)" || return 1
  else
    response="$(curl -fsSL --max-time 20 \
      -H 'Accept: application/vnd.github+json' \
      -H 'X-GitHub-Api-Version: 2022-11-28' \
      "https://api.github.com/repos/${UPSTREAM_REPO}/releases/latest" 2>/dev/null)" || return 1
  fi
  python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("tag_name", "")); print(d.get("html_url", "")); print((d.get("body") or "").replace("\r", ""))' <<<"$response"
}

show_status() {
  fetch_upstream true
  local baseline fork_release official_data official_tag official_url official_notes
  baseline="$(current_official_baseline)"
  fork_release="$(current_fork_release)"

  printf '\nCurrent checkout\n'
  printf '  branch:            %s\n' "$(git branch --show-current || printf detached)"
  printf '  fork release:      %s\n' "$fork_release"
  printf '  official baseline: %s\n' "$baseline"
  printf '  HEAD:              %s %s\n' "$(git rev-parse --short HEAD)" "$(git log -1 --format=%s)"

  if official_data="$(latest_official_release)"; then
    official_tag="$(sed -n '1p' <<<"$official_data")"
    official_url="$(sed -n '2p' <<<"$official_data")"
    official_notes="$(sed -n '3,$p' <<<"$official_data" | head -n 30)"
  else
    official_tag="unknown"
    official_url=""
    official_notes=""
    log "warning: could not query GitHub Releases (network or API limit)" >&2
  fi

  printf '\nOfficial upstream\n'
  printf '  latest release:    %s\n' "${official_tag:-unknown}"
  [[ -n "$official_url" ]] && printf '  release page:      %s\n' "$official_url"
  if [[ "$baseline" != none && "$official_tag" != unknown ]]; then
    if (( $(version_cmp "$baseline" "$official_tag") < 0 )); then
      printf '  state:             official update available; review and merge before publishing\n'
    else
      printf '  state:             current fork baseline covers the latest official release\n'
    fi
  fi

  if [[ "$baseline" != none ]]; then
    printf '\nCommits from baseline to %s/main\n' "$UPSTREAM_REMOTE"
    git log --oneline --decorate "$baseline..$UPSTREAM_REMOTE/main" | head -n 30 || true
    printf '\nChanged files\n'
    git diff --stat "$baseline...$UPSTREAM_REMOTE/main" | tail -n 40 || true
    printf '\nDatabase migration changes\n'
    git diff --name-status "$baseline...$UPSTREAM_REMOTE/main" -- backend/migrations | head -n 40 || true
  fi

  if [[ -n "$official_notes" ]]; then
    printf '\nLatest release notes (first 30 lines)\n%s\n' "$official_notes"
  fi
}

prepare_release() {
  local tag branch
  [[ $# -eq 1 ]] || { usage; exit 2; }
  require_clean_worktree
  tag="$(normalize_official_tag "$1")"
  fetch_upstream
  require_official_tag "$tag"
  branch="sync/${tag#v}"

  if git show-ref --verify --quiet "refs/heads/$branch"; then
    die "branch already exists: $branch"
  fi
  if git merge-base --is-ancestor "$tag" HEAD; then
    log "$tag is already merged into the current branch; no prepare branch created"
    return 0
  fi

  git switch -c "$branch"
  if ! git merge --no-ff --no-edit "$tag"; then
    cat >&2 <<MERGE_EOF

[fork-release] Merge stopped with conflicts.
Resolve the conflicts, run the required tests, then commit the merge.
When ready, return to the intended release branch and run:
  ./scripts/fork-release.sh publish $tag
MERGE_EOF
    exit 1
  fi
  log "created $branch from $tag"
  log "review, test, and merge this branch before publishing"
}

next_fork_sequence() {
  local version="$1" max=0 value
  while IFS= read -r value; do
    [[ -n "$value" ]] || continue
    (( value > max )) && max="$value"
  done < <(git tag -l "fork-v${version}-*" | sed -E 's/.*-([0-9]+)$/\1/' | grep -E '^[0-9]+$' || true)
  printf '%d' "$((max + 1))"
}

publish_release() {
  local tag="" push=false version next_seq fork_tag release_version branch source_commit base_commit version_file
  local arg
  for arg in "$@"; do
    case "$arg" in
      --push) push=true ;;
      -*) die "unknown option: $arg" ;;
      *) [[ -z "$tag" ]] || die "only one official tag may be supplied"; tag="$arg" ;;
    esac
  done
  [[ -n "$tag" ]] || { usage; exit 2; }

  require_clean_worktree
  tag="$(normalize_official_tag "$tag")"
  if [[ "$push" == true ]]; then
    fetch_upstream
    fetch_origin_tags
  else
    fetch_upstream true
  fi
  require_official_tag "$tag"
  git merge-base --is-ancestor "$tag" HEAD \
    || die "$tag is not an ancestor of HEAD; merge the official release first"

  version="${tag#v}"
  version_file="$(tr -d '[:space:]' < "$FORK_VERSION_FILE")"
  [[ "$version_file" == "$version" ]] \
    || die "$FORK_VERSION_FILE contains '$version_file', expected '$version'"

  branch="$(git branch --show-current || true)"
  [[ -n "$branch" ]] || die "cannot publish from a detached HEAD"
  [[ -f .fork-release.json ]] || die "missing .fork-release.json"
  python3 -c 'import json; json.load(open(".fork-release.json", encoding="utf-8"))' \
    || die ".fork-release.json is not valid JSON"

  next_seq="$(next_fork_sequence "$version")"
  fork_tag="fork-v${version}-${next_seq}"
  release_version="${version}-ds.${next_seq}"
  git rev-parse -q --verify "refs/tags/$fork_tag" >/dev/null && die "tag already exists: $fork_tag"
  source_commit="$(git rev-parse HEAD)"
  base_commit="$(git rev-parse "$tag^{commit}")"

  git tag -a "$fork_tag" -m "Fork release ${release_version}" \
    -m "release_version: ${release_version}" \
    -m "upstream_version: ${version}" \
    -m "upstream_tag: ${tag}" \
    -m "upstream_commit: ${base_commit}" \
    -m "source_commit: ${source_commit}"

  printf 'fork_tag=%s\n' "$fork_tag"
  printf 'release_version=%s\n' "$release_version"
  printf 'upstream_version=%s\n' "$version"
  printf 'source_commit=%s\n' "$source_commit"
  printf 'image=ghcr.io/baisuipingan/sub2api:%s\n' "$release_version"

  if [[ "$push" == true ]]; then
    log "pushing $branch and $fork_tag to $ORIGIN_REMOTE"
    git "${GIT_NET_OPTS[@]}" push "$ORIGIN_REMOTE" "HEAD:refs/heads/$branch"
    git "${GIT_NET_OPTS[@]}" push "$ORIGIN_REMOTE" "refs/tags/$fork_tag"
    log "release workflow will now build the immutable image"
  else
    log "tag created locally; rerun with --push after reviewing it"
  fi
}

case "${1:-}" in
  status|official-check) show_status ;;
  prepare) shift; prepare_release "$@" ;;
  publish) shift; publish_release "$@" ;;
  help|-h|--help) usage ;;
  *) usage; exit 2 ;;
esac
