# internal/hcjson — provenance and modifications

This package is vendored from
[zerolog](https://github.com/rs/zerolog) v1.34.0 `internal/json`, MIT
licensed — see `LICENSE` in this directory. Copyright (c) 2017 Olivier
Poitrey. The upstream source is available in the Go module cache as
`github.com/rs/zerolog@v1.34.0/internal/json`.

## Why

> V2_DESIGN.md and V2_PLAN.md live on the `v2` branch until the v1.0
> cutover merges it to main; the references below resolve there.

V2_DESIGN.md §8 (encoder decision): fork the zerolog append-only JSON
encoder, keep zero third-party dependencies in the root module, and earn
production trust for it on the classic API before the v2 record core
lands. The full rationale and measurements live in V2_PLAN.md §3b.

## Modifications from the original

- Package renamed from `internal/json` to `internal/hcjson`; the `Encoder`
  value type and method set are kept.
- **Hybrid SWAR escape fast path** (`string.go`, `bytes.go`): strings of
  16+ bytes are scanned 8 bytes per iteration with SWAR predicates; clean
  printable-ASCII strings are appended in bulk, everything else falls back
  to the original table path (`appendStringTable`), which is kept verbatim
  as the correctness oracle for the property and fuzz tests. The zero-byte
  detector is the canonical `(x - 0x0101010101010101) &^ x &
  0x8080808080808080` form; see the comments there for why the `&^ x`
  term is load-bearing (V2_PLAN.md §05 documents the incident class).
- Trimmed to what happycontext encodes: single-value appends only —
  string/bytes escaping, ints/uints, floats, bool, time, duration, nil,
  markers/keys, and an `any` fallback via `encoding/json`. Slice/array
  append variants, net types, reflect helpers, and hex encoding were
  dropped.
- `JSONMarshalFunc` (a must-set package variable in zerolog) became an
  internal `jsonMarshal = json.Marshal` default.
- Added `AppendTimeRFC3339`: a per-second-cached RFC3339 formatter for
  the JSON sink's completion-time stamp.
- `AppendTime` no longer interprets zerolog's UNIX-format sentinels; it
  formats with the provided layout directly.

Everything else is byte-for-byte zerolog logic, including the escape
table and the complex-path UTF-8 handling (`\ufffd` replacement).

## Tests

`string_test.go` carries the 200k-case property test against the vendored
table reference, adversarial position/carrier tests, and the fuzz seeds
(`fuzz_test.go`). Ported zerolog unit tests cover the type appends. CI
runs the fuzz targets 60s each per push (well past the 1M-exec gate pace).
