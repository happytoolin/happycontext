# Tasks: add-first-party-json-sink

Two PRs to main, sequential (not stacked), then release. Stack depth: 0.

## 1. PR-A `feat: add first-party JSON sink with SWAR encoder` (target: main)

- [ ] Vendor `internal/hcjson` from zerolog v1.34.0 `internal/json`
      (types/string/time/bytes/base), MIT notice + attribution file
- [ ] Hybrid SWAR escape: chunk scan for len ≥ 16, table path otherwise;
      include the `\x7f` check; canonical `hasZero64` form
- [ ] Property test: 200k random byte strings vs the zerolog-table reference
- [ ] Golden test: parsed field-set equivalence with the current zerolog
      adapter for a fixed corpus (exception list: none expected beyond
      ordering)
- [ ] Fuzz target wired into CI (1M execs gate; start at 60s per run)
- [ ] `hc.NewJSONSink(io.Writer)` on the current Sink interface;
      precomputed level prefixes; RFC3339 time via cached-second formatter
- [ ] Benches: add `BenchmarkJSONSink` cases; jsontext comparator behind
      `//go:build go1.27` in `benches/`
- [ ] Verify: full module matrix green, `-race` on root, benchstat vs the
      §4 gate (≤ 400 ns / ≤ 2 allocs for 12 fields), property test passing

## 2. PR-B `chore: go 1.25 floor, CI matrix with race and format gates` (target: main)

- [ ] Harmonize all `go.mod` directives to `go 1.25` (fiberv3 already there)
- [ ] CI: matrix {1.25.x, 1.26.x, 1.27.x}; add `-race` pass and `gofmt -l`
      gate to the existing all-modules loop
- [ ] Verify: CI green on all three toolchains

## 3. Release

- [ ] Merge PR-A, PR-B; let release-please open the v0.6.0 release PR
      (`feat:` → minor from 0.5.0; no breaking markers in scope)
- [ ] After release: merge main into `v2` so the branch carries the encoder
