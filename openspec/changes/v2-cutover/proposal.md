# v2 single-line release — publish v1.0.0 from v2

## Why

Zero external users means the dual-line choreography (classic main line,
port-back lane, cutover merge) protects a compatibility window that does
not exist. Owner decision 2026-08-31: `v2` becomes the single
development and release line; `main` freezes at v0.5.0 behind a pointer
banner. `V2_DESIGN.md` §9 is amended accordingly.

## What Changes

- Release workflow triggers on `v2` pushes only (exactly one branch may
  run release-please); CI gates PRs against `v2` as well as `main`.
- v0.6.0 (`add-first-party-json-sink`) releases **from v2**: PR-A/PR-B
  retarget to v2, release-please computes 0.5.0 → 0.6.0 there.
- The v1.0.0 path is unchanged in mechanism: the record-core breaking
  markers compute 0.6.x → 1.0.0 on v2; lockstep scripts tag all nested
  modules; no cutover merge, no classic-line retirement PR.
- `main` keeps the 0.x tags and a banner; afterwards the default branch
  moves to v2 (or main is fast-forwarded once — a plain merge).

## Non-goals

- v1.1 features (BufferedSink, watchdog, `adapter/otlp` evaluation) —
  sequenced after 1.0.0 as fresh changes.
- Renaming the module or moving to `/v2` import paths.
- Any change to the record-core work itself (`v2-record-core` is
  unaffected: it already targets `v2`).

## Impact

- Affected specs: `release-process` (branch references retargeted).
- Affected code: CI/release workflows only.
- Risk: release automation surprises — mitigated by verifying the v0.6.0
  release end-to-end on v2 before any breaking work lands.
