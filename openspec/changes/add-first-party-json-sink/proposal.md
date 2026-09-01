# Add first-party JSON sink (v0.6.0)

## Why

The v2 architecture's riskiest component is the forked encoder. Shipping it
additively on the classic API (v0.6.0) earns production trust before any
breaking change, and gives users a zero-dependency output path today — no
logger library required. Full rationale: `V2_DESIGN.md` §1 (path) and §5
amendment 10.

## What Changes

- Vendor zerolog v1.34.0's `internal/json` into `internal/hcjson` (MIT,
  attributed), trimmed to what we use, upgraded with a hybrid SWAR escape
  fast path (pure Go, `-11–18%` on clean ASCII, measured in `V2_PLAN.md` §3b).
- Add `hc.NewJSONSink(io.Writer)` implementing the **current** Sink interface
  (map in, encoded line out) — additive, non-breaking.
- Add the jsontext comparator in `benches/` behind a `go1.27` build tag.
- Harmonize Go floors to 1.25 across all modules; CI matrix 1.25/1.26/1.27
  with `-race` and a gofmt gate.
- Release v0.6.0 from v2 via release-please (`feat:` → minor); v2 is
  the single release line (§9 amendment, 2026-08-31).

## Non-goals

- Any change to the existing public API, adapters, or integrations.
- The typed record core, Record/Sink contract, or sampler changes — those
  are the `v2-record-core` change on the `v2` branch.

## Impact

- Affected specs: `json-encoder` (new capability).
- Affected code: new `internal/hcjson/`; root `sink` surface gains
  `NewJSONSink`; `benches` comparator; all `go.mod` files; CI workflow.
- Risk: encoder correctness — mitigated by property tests vs the zerolog
  table, fuzzing, and golden equivalence with the current zerolog adapter.
