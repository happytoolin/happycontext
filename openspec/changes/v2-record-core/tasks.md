# Tasks: v2-record-core

Four stacked PRs to `v2` (each rebased on its parent), plus an opportunistic
port-back lane. Every PR ships with the full module matrix, `-race`, and
benchstat evidence against the `V2_DESIGN.md` §4 gates.

## 1. PR-S1 `feat(core)!: typed WAL record core` (target: v2, stack base)

- [ ] `Field` type: kind discriminator + typed slots; `Kind()`/`Str()`/
      `Int()`/`Float()`/`Bool()`/`Time()`/`Duration()`/`Err()`/`Raw()`/`Any()`
- [ ] Event WAL: pooled, request-confined, pure-append `Add`; sealed-flag
      after `End` (straggler writes are no-ops, debug logs dropped keys);
      atomic armed-flag load per append; guarded-mode mutex only when armed
- [ ] Encode-time last-write-wins dedupe (seen-set allocated only when
      duplicates exist) — no Add-time scan
- [ ] `Compile`/`MustCompile` → `*Runtime`; sentinels `ErrInvalidRate`,
      `ErrInvalidLevel`, `ErrInvalidOutcome`; `%w` + `"hc: "` prefixes;
      nil sink = documented no-op runtime
- [ ] `Record`: `Level`/`Message`/`Fields`/`Lookup`/`Encoded` (lazy-once,
      atomic publish); completion-time RFC3339 via cached-second formatter
- [ ] `Level` int type with `String()`; `Add` family returns nothing;
      `AddRawJSON`; remove the v0 symbol list (`V2_DESIGN.md` §2)
- [ ] One-shot `End` (second call no-op returning first result; bool =
      event emitted; pool return wrapped against sink panics)
- [ ] Root tests: full matrix + `-race`; request-confined contract tests
- [ ] Benches module: port to new API (incl. go1.27 jsontext comparator)
- [ ] Verify gates: lifecycle ≤ 250 ns / ≤ 4 allocs; dropped End-path
      ≤ 100 ns; escape 96-char ≤ 26 ns; property + fuzz suites green

## 2. PR-S2 `feat(lifecycle): single lifecycle + integrations` (stacked on S1)

- [ ] Internalize begin/finish path; `hc.Start(ctx, rt, start)` +
      `op.End(&err)` only; delete hydration/reconciliation (~100 LOC)
- [ ] Sampler: `SampleInput` scalars + `Lookup` + `Fields()`; chain
      helpers unchanged; error/panic bypass enforced before custom sampler
- [ ] Outcome precedence: panic > error > explicit > 5xx > success
- [ ] Add `replace … => ../` to all nested modules' go.mod on the branch
- [ ] Port `common`, `std`, `gin`, `echo`, `fiber`, `fiberv3`, `worker`
      to `*Runtime` constructors; keep per-framework semantics
- [ ] Verify: integration suites green + `-race`; router benchmarks
      captured for the middleware ≤ 350 ns gate

## 3. PR-S3 `feat(adapters): bridges on []Field + canonical fields` (stacked on S2)

- [ ] slog/zap/zerolog bridges: iterate `rec.Fields()` in order → native
      typed constructors; remove `SinkOptions`/`NewWithOptions`; zerolog
      bridge may serve `rec.Encoded()` directly
- [ ] `Sink.Write(ctx, rec)` everywhere (slog.Handle shape; request ctx
      at commit, background from watchdog/drainer paths later)
- [ ] Field dedupe (W9): `op.*` canonical; `op.code` non-HTTP only;
      worker `job.*` mirrors dropped; `TestSink` captures `[]Field`
- [ ] Verify: bridge gates ≤ 900/450/300 ns (marked estimates — measure,
      then confirm or renegotiate per §6); golden field-set tests vs v0.6

## 4. PR-S4 `docs: examples, migration guide, README` (stacked on S3)

- [ ] `example_test.go`: output-checked Examples for `Compile`,
      `MustCompile`, `Start`/`End`, `Add` family, `NewJSONSink`,
      `NewTestSink`, one bridge, one integration
- [ ] `MIGRATION.md`: the v0→v1 map from `V2_DESIGN.md` §2 plus the
      outcome-precedence and field-rename callouts
- [ ] README v2 rewrite (quick starts from the spec's §2 samples)
- [ ] Verify: `go test` runs examples; docs build; full matrix final pass

## 5. Port-back lane (target: main, opportunistic — never stacked)

- [ ] After each PR-S merges: cherry-pick compatible items to main
      (encoder/SWAR improvements, property+fuzz suites, bench harness,
      docs, CI) as small `chore:`/`test:` PRs feeding the next v0.6.x
- [ ] Never port: API-shape work (S1–S3) or pool-dependent internals
      (sealing, arming, timeline, watchdog)
