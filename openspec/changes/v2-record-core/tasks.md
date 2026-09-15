# Tasks: v2-record-core

Four stacked PRs to `v2` (each rebased on its parent). Every PR ships
with the full module matrix, `-race`, and benchstat evidence against the
`V2_DESIGN.md` §4 gates. (The opportunistic port-back lane to main is
retired with the §9 amendment, 2026-08-31 — v2 is the only line.)

## 1. PR-S1 `feat(core)!: typed WAL record core` (target: v2, stack base)

- [x] `Field` type: kind discriminator + typed slots; `Kind()`/`Str()`/
      `Int()`/`Float()`/`Bool()`/`Time()`/`Duration()`/`Err()`/`Raw()`/`Any()`
- [x] Event WAL: pooled, request-confined, pure-append `Add`; sealed-flag
      after `End` (straggler writes are no-ops, debug logs dropped keys);
      atomic armed-flag load per append; guarded-mode mutex only when armed
- [x] Encode-time last-write-wins dedupe (seen-set allocated only when
      duplicates exist) — no Add-time scan
- [x] `Compile`/`MustCompile` → `*Runtime`; sentinels `ErrInvalidRate`,
      `ErrInvalidLevel`, `ErrInvalidOutcome`; `%w` + `"hc: "` prefixes;
      nil sink = documented no-op runtime
- [x] `Record`: `Level`/`Message`/`Fields`/`Lookup`/`Encoded` (lazy-once,
      atomic publish); completion-time RFC3339 via cached-second formatter
- [x] `Level` int type with `String()`; `Add` family returns nothing;
      `AddRawJSON`; remove the v0 symbol list (`V2_DESIGN.md` §2)
- [x] One-shot `End` (second call no-op returning first result; bool =
      event emitted; pool return wrapped against sink panics)
- [x] Root tests: full matrix + `-race`; request-confined contract tests
- [x] Benches module: port to new API (incl. go1.27 jsontext comparator)
- [x] Verify gates: lifecycle ≤ 250 ns / ≤ 4 allocs; dropped End-path
      ≤ 100 ns; escape 96-char ≤ 26 ns; property + fuzz suites green

## 2. PR-S2 `feat(lifecycle): single lifecycle + integrations` (stacked on S1)

- [x] Internalize begin/finish path; `hc.Start(ctx, rt, start)` +
      `op.End(&err)` only; delete hydration/reconciliation (~100 LOC)
      (achieved in PR-S1: the v0 begin/finish/hydration machinery was
      removed with the core rewrite)
- [x] Sampler: `SampleInput` scalars + `Lookup` + `Fields()`; chain
      helpers unchanged; error/panic bypass enforced before custom sampler
- [x] Outcome precedence: panic > error > explicit > 5xx > success
      (landed in PR-S1)
- [x] Add `replace … => ../` to all nested modules' go.mod on the branch
- [x] Port `common`, `std`, `gin`, `echo`, `fiber`, `fiberv3`, `worker`
      to `*Runtime` constructors; keep per-framework semantics
- [x] Verify: integration suites green + `-race`; router benchmarks
      captured for the middleware ≤ 350 ns gate

## 3. PR-S3 `feat(adapters): bridges on []Field + canonical fields` (stacked on S2)

- [x] slog/zap/zerolog bridges: iterate `rec.Fields()` in order → native
      typed constructors; remove `SinkOptions`/`NewWithOptions`; zerolog
      bridge may serve `rec.Encoded()` directly
- [x] `Sink.Write(ctx, rec)` everywhere (slog.Handle shape; request ctx
      at commit, background from watchdog/drainer paths later)
- [x] Field dedupe (W9): `op.*` canonical; `op.code` non-HTTP only;
      worker `job.*` mirrors dropped; `TestSink` captures `[]Field`
- [x] Verify: bridge gates ≤ 900/450/300 ns — measured with host-floor
      decomposition (see PR): slog 983-1080n/1al (floor 855), zap
      819n/1al (floor 677), zerolog 350-377n/0al (floor 204); the
      estimates under-modeled host-encoder cost — RENEGOTIATED per §6
      after two honest attempts (typed constructors + attr pool);
      golden field-set tests vs the zerolog bridge green (9 corpus
      cases)

## 4. PR-S4 `docs: examples, migration guide, README` (stacked on S3)

- [x] `example_test.go`: output-checked Examples for `Compile`,
      `MustCompile`, `Start`/`End`, `Add` family, `NewJSONSink`,
      `NewTestSink`, one bridge, one integration
- [x] `MIGRATION.md`: the v0→v1 map from `V2_DESIGN.md` §2 plus the
      outcome-precedence and field-rename callouts
- [x] README v2 rewrite (quick starts from the spec's §2 samples)
- [x] Verify: `go test` runs examples; docs build; full matrix final pass
