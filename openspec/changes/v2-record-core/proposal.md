# v2 record core (the break, on the `v2` branch)

## Why

The classic map-based core costs ~615 ns and 14 allocs per request, forces a
clone per kept write, and produces nondeterministic field order. The v2
record core replaces it with a per-request in-memory WAL of typed fields —
one representation from `hc.Add` to the wire, committed once at `op.End`.
Full design, measurements, and the 21 amendments: `V2_DESIGN.md`.

## What Changes

Delivered as **four stacked PRs to the `v2` branch** (stack depth 4 —
deliberately shallow). Per the §9 amendment (2026-08-31) v2 is the only
line — nothing flows to main:

- **PR-S1 — core**: typed `Field`/record WAL (append-only, sealed after
  `End`, atomic arming protocol), pooled events, encode-time last-wins
  dedupe, `Compile`/`MustCompile` + `Runtime` + error sentinels,
  `Record`/`Field` read API with lazy-once `Encoded()`, int-backed `Level`,
  no-op `Add` family, `AddRawJSON`. Old root API removed in this PR.
- **PR-S2 — lifecycle + integrations**: `Start`/`End` as the only
  lifecycle (begin/finish path internal), sampler `Lookup` + `Fields()`,
  all six integrations + `common` on `*Runtime` via local replaces.
- **PR-S3 — bridges + canonical fields**: slog/zap/zerolog bridges consume
  `[]Field` (no reflection, no sort, `SinkOptions`/`NewWithOptions`
  removed); field dedupe (`op.*` canonical, `op.code` non-HTTP only,
  worker `job.*` mirrors dropped).
- **PR-S4 — examples + migration**: runnable `example_test.go` gate,
  `MIGRATION.md` with the full v0→v1 map, README rewrite.

## Non-goals

- BufferedSink, stall watchdog, timeline (v1.1+; `v2_DESIGN.md` §8 ledger).
- OTel correlation or any destination adapter (parking lot).
- Any change to frozen main (retired with the port-back lane, §9 amendment).

## Impact

- Affected specs: `request-wal`, `record-sink`, `configuration`,
  `lifecycle` (all new capabilities; the classic line's behavior is
  superseded, not delta-modified — no baseline specs exist).
- Affected code: every module. Nested modules gain
  `replace github.com/happytoolin/happycontext => ../` on the branch;
  the lockstep release tooling strips them at cutover.
- Gates: `V2_DESIGN.md` §4 (lifecycle ≤ 250 ns/4 allocs, dropped ≤ 100 ns,
  middleware ≤ 350 ns, escape ≤ 26 ns; every PR carries benchstat evidence).
