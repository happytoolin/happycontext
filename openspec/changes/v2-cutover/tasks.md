# Tasks: v2-cutover

Sequential PRs on main, no stacking. Prerequisite: `v2-record-core` fully
merged on the `v2` branch.

## 1. PR-F1 `docs: announce classic line end` (target: main)

- [ ] After the final planned v0.6.x port-back release, freeze features
- [ ] README banner: final classic release; v1.0.0 (v2 record core) next;
      link `MIGRATION.md` (published from the v2 branch's PR-S4)
- [ ] Release the final classic version via release-please and verify tags

## 2. PR-F2 `feat!: v2 record core (W3–W9)` (target: main, from `v2`)

- [ ] Pre-flight: v2 branch green on full matrix + `-race`; §4 gates
      evidenced in the PR body; `MIGRATION.md` reviewed
- [ ] Merge `v2` into main with the breaking-marker title
- [ ] Confirm release-please opens `chore(main): release 1.0.0` — if it
      computes anything else, stop and reconcile the manifest before merge
      (the v0.5.0 lesson)
- [ ] Verify the lockstep workflow: `RELEASE_TAG=v1.0.0`, nested module
      tags created, branch-local replaces stripped
- [ ] Verify `go list -m -versions` picks up root + nested 1.0.0; smoke
      `go get github.com/happytoolin/happycontext@v1.0.0` in a scratch
      module against the README quick start

## 3. Post-cutover

- [ ] Archive the three OpenSpec changes (`openspec archive …`) so main
      specs absorb the deltas
- [ ] Open the v1.1.0 parking-lot review (BufferedSink, watchdog,
      `adapter/otlp`) as fresh changes — nothing starts without them
