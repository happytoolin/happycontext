#!/usr/bin/env bash
# Shared discovery for lockstep sync, git add, and publish.
# Source this file; do not execute it.

# Nested modules whose go.mod require the root (adapters, integrations,
# plus benches/examples which are not published as versioned modules).
lockstep_require_modfiles() {
  git ls-files 'adapter/*/go.mod' 'integration/*/go.mod' 'benches/go.mod' 'cmd/examples/go.mod' | sort
}

# Modules that receive adapter/<path>/vX.Y.Z and integration/<path>/vX.Y.Z tags.
publishable_module_dirs() {
  git ls-files 'adapter/*/go.mod' 'integration/*/go.mod' | sed 's#/go\.mod$##' | sort
}

# Modules refreshed in the Go module proxy after a release: the root
# plus the tagged nested modules (benches/examples are not published).
published_modfiles() {
  printf '%s\n' go.mod
  git ls-files 'adapter/*/go.mod' 'integration/*/go.mod' | sort
}
