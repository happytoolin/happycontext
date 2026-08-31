# Tasks: v2-cutover

Single release line on `v2` (V2_DESIGN.md §9 amendment, 2026-08-31).
No cutover merge and no classic-line freeze: `main` is frozen at v0.5.0
behind a pointer banner. Prerequisite: `add-first-party-json-sink`
merged on `v2` and v0.6.0 released there.

## 1. Release line switch (target: v2)

- [ ] Choreography PR: release workflow triggers on v2 only; CI gates
      v2 PRs; V2_DESIGN §9 + ledger amended; main banner PR open
- [ ] Merge PR-A/PR-B (retargeted to v2); release v0.6.0 via
      release-please on v2; verify the release PR computes 0.5.0 →
      0.6.0 and the lockstep workflow tags nested modules v0.6.0 (the
      v0.5.0 lesson: never merge a release PR with a surprising
      version)

## 2. v1.0.0 (target: v2, after `v2-record-core`)

- [ ] Pre-flight: v2 green on full matrix + `-race`; §4 gates evidenced
      in the final PR; `MIGRATION.md` reviewed
- [ ] Merge the final `v2-record-core` PRs carrying breaking markers;
      confirm release-please opens `chore(release): 1.0.0` — anything
      else, stop and reconcile the manifest before merging
- [ ] Verify lockstep: `RELEASE_TAG=v1.0.0`, nested module tags created,
      branch-local replaces stripped; smoke `go get
      github.com/happytoolin/happycontext@v1.0.0` in a scratch module
      against the README quick start

## 3. Post-1.0

- [ ] Archive the three OpenSpec changes (`openspec archive …`) so main
      specs absorb the deltas
- [ ] Open the v1.1.0 parking-lot review (BufferedSink, watchdog,
      `adapter/otlp`) as fresh changes — nothing starts without them
- [ ] Owner decision: switch the GitHub default branch to v2, or fast
      forward main to v2 once (plain merge; main never releases again)
