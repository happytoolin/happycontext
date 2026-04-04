#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: ./scripts/publish-lockstep-release.sh [--push] [--remote <name>] [--github-releases] <version> [target]

Creates module tags/releases for every publishable submodule at the same version
as the root release. By default tags point at HEAD unless an explicit target is
provided.

Examples:
  ./scripts/publish-lockstep-release.sh v0.2.3
  ./scripts/publish-lockstep-release.sh --push --github-releases v0.2.3
  ./scripts/publish-lockstep-release.sh --push v0.2.3 origin/main
EOF
}

remote=origin
push_tags=false
create_releases=false

while (($# > 0)); do
  case "$1" in
    --push)
      push_tags=true
      shift
      ;;
    --remote)
      if (($# < 2)); then
        echo "--remote requires a value" >&2
        exit 1
      fi
      remote="$2"
      shift 2
      ;;
    --github-releases)
      create_releases=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      break
      ;;
  esac
done

if (($# < 1 || $# > 2)); then
  usage >&2
  exit 1
fi

version="$1"
version="v${version#v}"
target="${2:-HEAD}"

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

publishable_modules() {
  git ls-files 'adapter/*/go.mod' 'integration/*/go.mod' | sed 's#/go\.mod$##' | sort
}

declare -a created_tags=()

create_tag_if_missing() {
  local tag_name="$1"
  local tag_target="$2"
  local message="$3"

  if git rev-parse -q --verify "refs/tags/$tag_name" >/dev/null; then
    echo "exists: $tag_name"
    return
  fi

  git tag -a "$tag_name" "$tag_target" -m "$message"
  created_tags+=("$tag_name")
  echo "created: $tag_name -> $tag_target"
}

create_release_if_missing() {
  local tag_name="$1"
  local title="$2"
  local notes="$3"

  if gh release view "$tag_name" >/dev/null 2>&1; then
    echo "release exists: $tag_name"
    return
  fi

  gh release create "$tag_name" --title "$title" --notes "$notes" --verify-tag
}

while IFS= read -r module_path; do
  tag_name="${module_path}/${version}"
  create_tag_if_missing "$tag_name" "$target" "Release $tag_name"
done < <(publishable_modules)

if [[ "$push_tags" == true && ${#created_tags[@]} -gt 0 ]]; then
  git push "$remote" "${created_tags[@]}"
fi

if [[ "$create_releases" == true ]]; then
  while IFS= read -r module_path; do
    tag_name="${module_path}/${version}"
    create_release_if_missing \
      "$tag_name" \
      "${module_path}: ${version}" \
      "Lockstep module release for \`${module_path}\` at ${version}."
  done < <(publishable_modules)
fi
