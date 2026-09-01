# Tasks: add-first-party-json-sink

Two PRs, then release, on the single v2 release line (§9 amendment,
2026-08-31). PR-B is stacked on PR-A — merge order is fixed:
**#26 (release line) → PR-A → PR-B**. Stack depth: 1.

## 1. PR-A `feat: add first-party JSON sink with SWAR encoder` (target: v2)

- [x] Vendor `internal/hcjson` from zerolog v1.34.0 `internal/json`
      (types/string/time/bytes/base), MIT notice + attribution file
- [x] Hybrid SWAR escape: chunk scan for len ≥ 16, table path otherwise;
      include the `\x7f` check; canonical `hasZero64` form
- [x] Property test: 200k random byte strings vs the zerolog-table reference
- [x] Golden test: parsed field-set equivalence with the current zerolog
      adapter for a fixed corpus (exception list: none expected beyond
      ordering)
- [x] Fuzz target wired into CI (1M execs gate; start at 60s per run)
- [x] `hc.NewJSONSink(io.Writer)` on the current Sink interface;
      precomputed level prefixes; RFC3339 time via cached-second formatter
- [x] Benches: add `BenchmarkJSONSink` cases; jsontext comparator behind
      `//go:build go1.27` in `benches/`
- [x] Verify: full module matrix green, `-race` on root, benchstat vs the
      §4 gate (≤ 400 ns / ≤ 2 allocs for 12 fields), property test passing

## 2. PR-B `chore: go 1.25 floor, CI matrix with race and format gates` (target: v2)

- [ ] Harmonize all `go.mod` directives to `go 1.25` (fiberv3 already there)
- [ ] CI: matrix {1.25.x, 1.26.x, 1.27.x}; add `-race` pass and `gofmt -l`
      gate to the existing all-modules loop
- [ ] Verify: CI green on all three toolchains

## 3. Release

- [ ] Merge PR-A, PR-B into v2; let release-please open the v0.6.0
      release PR on v2 (`feat:` → minor from 0.5.0; no breaking markers
      in scope)
- [ ] After release: nothing to sync — v2 is the release line (the old
      "merge main into v2" step is retired with the §9 amendment)
