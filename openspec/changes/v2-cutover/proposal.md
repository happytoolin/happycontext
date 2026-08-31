# v2 cutover — final classic release, then publish v1.0.0

## Why

The classic line must end deliberately (with the migration guide already
published) and the v2 architecture must publish as **v1.0.0** — reserving
`/v2` module-path churn and making the stability promise at the moment
the new API lands. Choreography: `V2_DESIGN.md` §9.

## What Changes

- Feature-freeze main after the last v0.6.x; final classic release with a
  README banner pointing to `MIGRATION.md`.
- One cutover PR merges `v2` into main as `feat!: v2 record core (W3–W9)`.
  Release-please computes **0.6.x → 1.0.0** from the breaking marker — the
  same mechanism hand-corrected at v0.5.0, now working for us.
- Lockstep scripts strip branch-local `replace` directives, tag root
  `v1.0.0` and every nested module `<path>/v1.0.0`.
- README/docs swap to v2; the classic line remains installable as 0.x tags.

## Non-goals

- v1.1 features (BufferedSink, watchdog, `adapter/otlp` evaluation) —
  trunk-based after cutover.
- Renaming the module or moving to `/v2` paths.

## Impact

- Affected specs: `release-process` (new capability).
- Affected code: none beyond merges — all code landed via `v2-record-core`.
- Risk: release automation surprises — mitigated by verifying the v0.6.0
  release flow end-to-end first and watching the cutover workflow's
  `RELEASE_TAG`.
