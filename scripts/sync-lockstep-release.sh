#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: ./scripts/sync-lockstep-release.sh <version>

Updates local module references to the supplied root release version.
Examples:
  ./scripts/sync-lockstep-release.sh v0.2.3
  ./scripts/sync-lockstep-release.sh 0.2.3
EOF
}

if (($# != 1)); then
  usage >&2
  exit 1
fi

version="$1"
version="${version#v}"

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"
# shellcheck source=scripts/lockstep-modules.sh
source "$repo_root/scripts/lockstep-modules.sh"

update_root_requirement() {
  local file="$1"

  if ! grep -q 'github.com/happytoolin/happycontext v' "$file"; then
    return
  fi

  perl -0pi -e "s#github\\.com/happytoolin/happycontext v\\d+\\.\\d+\\.\\d+#github.com/happytoolin/happycontext v${version}#g" "$file"
}

while IFS= read -r modfile; do
  update_root_requirement "$modfile"
done < <(lockstep_require_modfiles)
