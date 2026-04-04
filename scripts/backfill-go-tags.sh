#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: ./scripts/backfill-go-tags.sh [--push] [--remote <name>] [version ...]

Creates the Go-compatible tags that should exist alongside legacy
happycontext-vX.Y.Z tags:

  vX.Y.Z
  adapter/slog/vX.Y.Z
  adapter/zap/vX.Y.Z
  adapter/zerolog/vX.Y.Z
  integration/echo/vX.Y.Z
  integration/fiber/vX.Y.Z
  integration/fiberv3/vX.Y.Z
  integration/gin/vX.Y.Z
  integration/std/vX.Y.Z

Examples:
  ./scripts/backfill-go-tags.sh
  ./scripts/backfill-go-tags.sh v0.2.0
  ./scripts/backfill-go-tags.sh happycontext-v0.2.0 --push
EOF
}

remote=origin
push_tags=false
declare -a requested_versions=()
declare -a module_paths=()

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
    -h|--help)
      usage
      exit 0
      ;;
    *)
      requested_versions+=("$1")
      shift
      ;;
  esac
done

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

map_legacy_tag() {
  local value="$1"
  if [[ "$value" == happycontext-v* ]]; then
    printf '%s\n' "$value"
    return
  fi
  if [[ "$value" == v* ]]; then
    printf 'happycontext-%s\n' "$value"
    return
  fi
  printf 'happycontext-v%s\n' "$value"
}

publishable_modules() {
  git ls-files 'adapter/*/go.mod' 'integration/*/go.mod' | sed 's#/go\.mod$##' | sort
}

declare -a legacy_tags=()

if ((${#requested_versions[@]} > 0)); then
  for version in "${requested_versions[@]}"; do
    legacy_tag="$(map_legacy_tag "$version")"
    if ! git rev-parse -q --verify "refs/tags/$legacy_tag" >/dev/null; then
      echo "missing legacy tag: $legacy_tag" >&2
      exit 1
    fi
    legacy_tags+=("$legacy_tag")
  done
else
  while IFS= read -r tag; do
    legacy_tags+=("$tag")
  done < <(git tag --list 'happycontext-v*' | sort -V)
fi

if ((${#legacy_tags[@]} == 0)); then
  echo "no legacy happycontext-v* tags found"
  exit 0
fi

declare -a created_tags=()

create_tag_if_missing() {
  local tag_name="$1"
  local target="$2"
  local message="$3"

  if git rev-parse -q --verify "refs/tags/$tag_name" >/dev/null; then
    echo "exists: $tag_name"
    return
  fi

  git tag -a "$tag_name" "$target" -m "$message"
  created_tags+=("$tag_name")
  echo "created: $tag_name -> $target"
}

while IFS= read -r module_path; do
  module_paths+=("$module_path")
done < <(publishable_modules)

for legacy_tag in "${legacy_tags[@]}"; do
  version="${legacy_tag#happycontext-}"
  target="$(git rev-list -n1 "$legacy_tag")"

  create_tag_if_missing "$version" "$target" "Release $version"

  for module_path in "${module_paths[@]}"; do
    if git cat-file -e "$target:$module_path/go.mod" 2>/dev/null; then
      create_tag_if_missing \
        "$module_path/$version" \
        "$target" \
        "Release $module_path/$version"
    fi
  done
done

if ((${#created_tags[@]} == 0)); then
  echo "no tags created"
  exit 0
fi

if [[ "$push_tags" == true ]]; then
  git push "$remote" "${created_tags[@]}"
fi
