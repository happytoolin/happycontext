# release-process Specification (delta)

## ADDED Requirements

### Requirement: Breaking releases compute the major
A release PR containing a breaking conventional-commit marker SHALL
compute a major version bump via release-please; maintainers SHALL
verify the computed version before merging any release PR. The release
branch is `v2` (§9 amendment); release-please runs on v2 pushes only.

#### Scenario: Record-core bump
- GIVEN v2 at 0.6.x and a record-core commit titled `feat!: v2 record core`
- WHEN release-please runs
- THEN the release PR proposes `1.0.0`

#### Scenario: Version mismatch guard
- GIVEN a release PR computing an unexpected version
- WHEN a maintainer notices before merge
- THEN the merge halts until the manifest and commit history reconcile

### Requirement: Lockstep module tags
Every root release SHALL tag all nested `adapter/*` and `integration/*`
modules at the same version via the lockstep scripts. Sync, `git add`,
and publish SHALL share one discovery list (`git ls-files` globs).
Branch-local `replace` directives stay in the repository so CI can
test against the local root; GOPROXY consumers ignore `replace` and
resolve the updated `require` line.

#### Scenario: Nested module discovery
- GIVEN the v1.0.0 release completed
- WHEN a user runs `go list -m -versions github.com/happytoolin/happycontext/adapter/slog`
- THEN `v1.0.0` appears in the version list
