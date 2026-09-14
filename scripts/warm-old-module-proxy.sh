#!/usr/bin/env bash
# Force proxy.golang.org to cache every published happycontext module
# version before the repository is renamed to unolog.
#
# After the GitHub rename, the old module path no longer resolves: the
# go-import meta tag advertises github.com/happytoolin/unolog, so the
# proxy cannot fetch a version it has never seen. Versions already in
# the proxy cache are immutable and stay downloadable forever. Run this
# script BEFORE renaming the repository; it is safe to re-run.
#
# Usage:
#   ./scripts/warm-old-module-proxy.sh
#
# Env:
#   PROXY   module proxy base URL (default https://proxy.golang.org)

set -euo pipefail

PROXY="${PROXY:-https://proxy.golang.org}"
OLD_PREFIX="github.com/happytoolin/happycontext"
NEW_PREFIX="github.com/happytoolin/unolog"

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

encode() { printf '%s' "$1" | sed 's#/#%2F#g'; }

warm_url() {
  local url="$1"
  if curl -fsS -o /dev/null --retry 3 --retry-delay 1 "$url"; then
    return 0
  fi
  echo "MISS $url" >&2
  return 1
}

warm_module() {
  local moddir="$1"
  local new_path old_path encoded prefix

  new_path="$(sed -n 's/^module //p' "$moddir/go.mod" | head -n1)"
  old_path="${new_path/#$NEW_PREFIX/$OLD_PREFIX}"
  encoded="$(encode "$old_path")"

  if [[ "$moddir" == "." ]]; then
    prefix=""
  else
    prefix="$moddir/"
  fi

  echo "== warming $old_path =="

  # Version listing and @latest are recomputed upstream once their cache
  # entries expire; seed them now while the repository still answers.
  warm_url "$PROXY/$encoded/@v/list" || true
  warm_url "$PROXY/$encoded/@latest" || true

  local tag version ext
  while IFS= read -r tag; do
    version="${tag#"$prefix"}"
    for ext in info mod zip; do
      warm_url "$PROXY/$encoded/@v/$version.$ext" || true
    done
  done < <(git tag --list "$prefix""v*" | sort -V)

  echo "done $old_path"
}

warm_module "."
while IFS= read -r modfile; do
  warm_module "$(dirname "$modfile")"
done < <(git ls-files 'adapter/*/go.mod' 'integration/*/go.mod' | sort)

echo
echo "All versions fetched. Pinned pre-1.0 builds stay resolvable after the rename;"
echo "exact versions are immutable proxy entries, while @latest/@v/list may stop"
echo "refreshing once their upstream cache expires."
