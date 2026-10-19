#!/usr/bin/env bash
set -Eeuo pipefail

source_root="$(cd "$(dirname "$0")/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
export HOME="$tmp/home"
mkdir -p "$HOME" "$tmp/upstream.git" "$tmp/origin.git" "$tmp/repo"
git init --bare --quiet "$tmp/upstream.git"
git init --bare --quiet "$tmp/origin.git"
git -C "$tmp/repo" init --quiet -b main
git -C "$tmp/repo" config user.name test
git -C "$tmp/repo" config user.email test@example.invalid
git -C "$tmp/repo" config commit.gpgsign false
git -C "$tmp/repo" remote add upstream "$tmp/upstream.git"
git -C "$tmp/repo" remote add origin "$tmp/origin.git"

mkdir -p "$tmp/repo/backend/cmd/server" "$tmp/repo/scripts/tests" "$tmp/repo/backend/migrations"
printf '0.2.18\n' > "$tmp/repo/backend/cmd/server/VERSION"
cp "$source_root/.fork-release.json" "$tmp/repo/.fork-release.json"
cp "$source_root/scripts/fork-release.sh" "$tmp/repo/scripts/fork-release.sh"
printf 'base\n' > "$tmp/repo/README.md"
git -C "$tmp/repo" add .
git -C "$tmp/repo" commit --quiet -m 'base release'
git -C "$tmp/repo" tag -a v0.2.18 -m 'official v0.2.18'
git -C "$tmp/repo" push --quiet upstream refs/heads/main refs/tags/v0.2.18
git -C "$tmp/repo" push --quiet origin refs/heads/main

cd "$tmp/repo"
./scripts/fork-release.sh publish v0.2.18 >/tmp/fork-release-publish.out
git rev-parse -q --verify refs/tags/fork-v0.2.18-1 >/dev/null
grep -F 'release_version=0.2.18-ds.1' /tmp/fork-release-publish.out >/dev/null

./scripts/fork-release.sh publish v0.2.18 >/tmp/fork-release-publish-2.out
git rev-parse -q --verify refs/tags/fork-v0.2.18-2 >/dev/null
grep -F 'release_version=0.2.18-ds.2' /tmp/fork-release-publish-2.out >/dev/null

printf 'dirty\n' > dirty.txt
if ./scripts/fork-release.sh publish v0.2.18 >/dev/null 2>&1; then
  echo 'dirty worktree unexpectedly published a tag' >&2
  exit 1
fi
rm dirty.txt

if ./scripts/fork-release.sh publish v9.9.9 >/dev/null 2>&1; then
  echo 'missing official tag unexpectedly published a tag' >&2
  exit 1
fi

printf 'fork-release shell tests passed\n'
