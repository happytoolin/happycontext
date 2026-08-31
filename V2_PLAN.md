# happycontext v2 — Faster, Cleaner, Simpler

Status: **research complete; design locked 2026-08-30 — the buildable
specification is now `V2_DESIGN.md`.** This document is the rationale
and measurement archive behind it. It separates API-compatible
performance work from changes reserved for the breaking release. The
focus is to avoid work that has no effect on the emitted event. It does
not propose a cache, a new configuration layer, or a new runtime
dependency.

## 1. Where we are

Measured baseline on main `d79a41b` (Apple M4, `benches` module, 3–5 runs,
2026-08-30):

| Benchmark | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| OperationLifecycle (full, kept) | ~615 | 1,648 | 14 |
| OperationLifecycleDropped (8 fields, rate 0) | ~628 | 1,240 | 9 |
| CommitPath | ~266 | 832 | 6 |
| EventAddMany (5 fields, incl. NewContext) | ~178 | 496 | 4 |
| Router std middleware (discard sink) | ~1,000 | 2,576 | 26 |
| Router std direct zerolog (nop logger) | ~67 | 208 | 4 |
| Adapter write medium (slog / zap / zerolog) | 1,699 / 858 / 354 | — | — |

Interpretation:

- The happycontext tax on a request is roughly 600–950 ns and 14–22 extra
  allocations, dominated by four things: `context.WithValue` + Event alloc,
  the `map[string]any` field store (boxing + growth), `maps.Clone` per kept
  write, and adapter conversion (`slog.Any` / `zap.Any` reflection, key
  sorting for deterministic order).
- Middleware with a discard sink is currently slower than plain slog JSON
  logging and ~15x direct zerolog. That is the gap v2 closes.
- The API surface (~40 exported symbols in the root) carries a parallel
  low-level lifecycle (`BeginOperation`/`FinishOperation`/`OperationFinish`)
  plus hydration/reconciliation logic kept only for it.

Where the ~615 ns lifecycle actually goes — profile-guided estimates
(pprof attribution from `BENCHMARK_REPORT.md`; per-item costs measured in
§3b). Ranges, not gospel — confirmed with pprof during implementation:

| Cost slice (v0.4.0) | ≈ ns | ≈ allocs | v2 removes it via |
| --- | ---: | ---: | --- |
| Context plumbing (`WithValue` + `req.WithContext`) | 40–70 | 2–3 | kept — it is the context contract |
| Event + map allocate & grow | 80–150 | 3–4 | W3 · pooled typed records |
| Scalar boxing into `any` | 30–60 | 2–5 | W3 · kind-discriminated slots |
| Policy path + per-finish validation | 30–60 | 0 | W5 · compiled runtime |
| Extra `time.Now()` reads | ~32 | 0 | §3b · reuse start-time reading |
| `maps.Clone` snapshot per kept write | 100–250 | 2–3 | W4 · zero-copy Record view |
| Hydration/reconciliation on finish | 20–40 | 0 | W7 · single lifecycle |
| Adapter conversion (real sinks, 12 fields) | 350–1,700 | 0–18 | W8 · typed → native fields; first-party sink ≈ 350–400 |

## 2. v2 thesis

One typed-field event core, one first-party writer, thin bridges.

1. **Event storage becomes a typed, insertion-ordered field slice.** No
   `map[string]any`, no boxing of scalars, no clone per write, no mutex.
   Deterministic field order becomes a property of the data structure, so
   the `DeterministicOrder` option, key pools, and sorting disappear from
   every adapter.
2. **happycontext owns its output path.** Fork zerolog's JSON encoder
   (MIT, ~800 LOC of non-test encoder code) into `internal/`, and ship
   `hc.NewJSONSink(io.Writer)` — zero third-party dependencies, single
   buffered write per event, byte-compatible with today's zerolog adapter
   output. Adapters remain for shops that already run slog/zap/zerolog, but
   new users need no logger dependency at all.
3. **One lifecycle.** `hc.Start` + `op.End`. The parallel
   `BeginOperation`/`FinishOperation` path becomes internal. Configuration
   is compiled once (`hc.Compile`) instead of re-derived per finish.

Why a zerolog fork and not zap or phuslu/log:

- zerolog's encoder is the smallest correct core (`internal/json`:
  types/string/time/bytes/base ≈ 800 LOC non-test) and its buffer-append
  style matches our append-at-`Add` model. Output stays byte-compatible
  with what zerolog adapter users already ingest.
- zap's encoder core is larger and reflection-centric; we would keep the
  part we don't need.
- phuslu/log is faster in microbenchmarks but has no format continuity with
  our existing output; the win we want (encoding at commit, no reflection)
  is available from the zerolog encoder either way.

Why not store fields as a pre-encoded JSON buffer (pure zerolog style):
bridges and custom samplers must be able to read fields without parsing.
A typed slice is readable by every consumer (built-in sink, bridges,
samplers) with no decode step, at a small constant cost over raw buffer
appends. This is the "faster AND simpler" trade: one representation,
zero conversion between core and sinks.

The same request, walked through both implementations:

| # | v0.4.0 (~1,000 ns / 26 allocs) | v2 (target ≤ 350 ns / ≤ 8 allocs) |
| --- | --- | --- |
| 1 | `context.WithValue` boxes the event, allocates | same `WithValue` (kept) + claim pooled event, append header records |
| 2 | allocate Event + `map(8)`, insert headers, box non-trivial ints | header records appended — no map, nothing boxes |
| 3 | handler `hc.Add` ×2: mutex, map writes, boxing | two typed record appends, ~10 ns each |
| 4 | finish: re-hydrate start fields, select policy, validate levels, second `time.Now()` | outcome + three completion records; compiled policy lookup; no second clock read |
| 5 | write completion fields into the map (`duration_ms` boxes int64) | same three records appended |
| 6 | `maps.Clone` the field map for the sink | `Record` wraps the buffer — zero copy |
| 7 | adapter: random map order, pool + sort keys, reflective conversion | one forward encode pass (SWAR), single write; or bridge iterates typed records |
| 8 | GC collects ~2.6 KB / 26 allocs later | buffer zeroed and pooled — near-zero steady garbage |

The emitted JSON line is the same shape either way — v2 changes what
happens before the line exists, not the line. The event was never really
a map that needed cloning, converting, and sorting; it was always an
ordered list of facts about one request.

## 3. Workstreams

### W1 — Fork the encoder + hybrid SWAR escape scan (S/M)

Vendor zerolog v1.34.0's `internal/json` package (MIT, attribution in
`internal/hcjson/LICENSE` + NOTICE) trimmed to what we use: string
escaping, ints/uints, floats, bool, time, duration, bytes, error, and an
`any` fallback via `encoding/json`. Port its test files; add golden tests
against the real zerolog adapter output and a short fuzz run in CI.

Upgrade the escape scan with a SWAR fast path (8 bytes per iteration,
pure Go, no assembly): per 64-bit word check `bytes < 0x20`, `"`, `\`,
`\x7f` via the underflow/zero-detect tricks, and punt to the table path
on any non-ASCII byte. Prototype numbers (M4, Go 1.27.0, property-tested
against the zerolog table on 200k random strings):

| Payload | zerolog table | hybrid SWAR | jsontext (stdlib) |
| --- | ---: | ---: | ---: |
| 7-char key | 4.8 ns | 5.2 ns | 7.1 ns |
| 22-char path | 9.2 ns | 8.2 ns | 12.6 ns |
| 96-char URL | 30.9 ns | 25.4 ns | ~50 ns |
| escape-heavy (2 quotes) | 22.2 ns | 25.4 ns | 22.3 ns |
| unicode | 25.3 ns | 25.7 ns | 21.6 ns |

Adopt as a hybrid: SWAR scan only for `len(s) >= 16`, table loop below
that and for suspicious chunks. Net effect ≈ −11–18% on clean ASCII (the
dominant log-field shape), small loss on escape-heavy strings.

Two hard-won correctness notes, both now plan gates:

- The zero-byte detector must be the canonical form
  `(x-0x0101..) &^ x & 0x8080..` — a common variant without `&^x`
  detects nothing, and the encoder still "works" while silently not
  escaping. My first prototype benchmarked 3× faster for exactly this
  reason; only a property test caught it. Benchmark numbers without an
  equivalence test are not evidence.
- `\x7f` must be in the chunk checks (zerolog's table escapes it).

Keep the encoder behind a tiny internal interface with a second backend
implemented on Go 1.27's `encoding/json/jsontext` (`AppendQuote`,
`AppendFloat`, no experiment flag needed). jsontext is ~45% slower than
the fork on clean strings and comparable on escape-heavy ones, so the
fork stays primary and jsontext becomes the maintenance-free fallback —
and the interface lets us re-race them per Go release.

Acceptance: property test vs the zerolog table (random bytes, edge
cases), `go test -fuzz` clean for 1M execs, golden equivalence with the
current zerolog adapter output for a fixed field corpus.

### W2 — First-party JSON sink on v0 storage (S) — shippable before the break

`hc.NewJSONSink(w io.Writer, opts...)` implementing today's `Sink`
interface (map in, encode out). Additive, non-breaking, lands as v0.5.0.
This puts the forked encoder in production early and lets users drop their
adapter dependency before v2 changes anything else.

Options: `Pretty` (dev console rendering can stay out of scope), timestamp
injection, level formatting to match current zerolog events.

### W3 — Typed Event core (M/L) — the breaking heart

```go
type fieldKind uint8 // KindString, KindInt, KindUint, KindFloat, KindBool,
                     // KindTime, KindDuration, KindErr, KindAny
type field struct {
    key  string
    kind fieldKind
    i    int64 // ints, bool, unixnano, duration
    f    float64
    s    string
    a    any // fallback
}
```

- `Event` holds `fields []field` plus a scalar header: `startTime`,
  `message`, `levelFloor`, `hasError`, and the sampling scalars (domain,
  name, method, path, status, code). Request-confined: **no mutex**
  (`PERFORMANCE_PLAN` item D). Concurrent use is documented as invalid;
  the race-test suite moves to per-request confinement tests.
- `hc.Add` appends typed fields — one WAL record per call. Zero boxing
  for scalars; slice growth is amortized and the event is pooled
  (`sync.Pool`) by the lifecycle, so steady-state allocs per request
  approach zero. Duplicate keys resolve last-write-wins (§3a).
- The event timestamp reuses the single start-time `time.Now()` reading
  (see §3b) — completion performs no clock calls of its own.
- Completion (`duration_ms`, `op.code`, `op.outcome`) appends three fields.
- Error/panic metadata becomes nested object fields appended by the
  encoder (same JSON shape as today's `error`/`panic` maps).

### W4 — Record + sink contract v2 (M)

`PERFORMANCE_PLAN` item A, realized on the slice:

```go
type Record struct { // read-only view, valid only during Write
    Level   Level
    Message string
    // Fields() []Field — insertion-ordered view
    // Lookup(key string) (any, bool) — linear scan, no alloc
}
type Sink interface { Write(rec *Record) }
```

- No owned map, no clone: 2–4 allocs and 300–2,400 B removed per kept
  event.
- Sinks must not retain the record (documented contract).
- `TestSink` captures into `[]CapturedEvent` for tests, same as today.

### W5 — Compiled runtime config (S/M)

`hc.Compile(Config) *Runtime` — clamp rates, validate levels/outcomes,
resolve the default-domain alias, precompute per-domain policy lookup.
Middleware/integrations compile once at construction and fail fast on bad
config. The finish path reads precomputed values only; `NormalizeConfig`
stays for compat or is dropped (see migration).

### W6 — Sampler v2 (S)

`PERFORMANCE_PLAN` item B: `SampleInput` drops `Event *Event` and gains
`Lookup(key) (any, bool)`. Everything else (`KeepErrors`,
`KeepPathPrefix`, `KeepSlowerThan`, `ChainSampler`, `RateSampler`,
error/panic retention) keeps its signature and semantics. Dropped events
skip completion-field appends entirely — that is where the ≤100 ns dropped
path comes from. Post-`End` field inspection of dropped events is no
longer guaranteed (documented behavior change).

### W7 — Single lifecycle (M)

- Keep: `StartOperation` (maybe renamed `hc.Start`) taking the compiled
  runtime, `op.End(&err)`, panic capture + re-panic, `op.Context()`.
- Remove from the public API: `BeginOperation`, `FinishOperation`,
  `OperationFinish`, `hydrateOperationStart`, and the
  `operationStartFieldsNeedUpdate` reconciliation (~100 LOC). They move to
  `internal/` or vanish.
- `Commit` becomes `op.Commit(level)` or stays a package function with the
  Record contract.
- Integrations (`std`, `gin`, `echo`, `fiber`, `fiberv3`, `worker`,
  `common`) all finalize through one internal path.

### W8 — Bridges rewrite (M)

slog/zap/zerolog adapters consume `[]Field` in insertion order:

- type-switch each field to native typed constructors (`zap.String`,
  `slog.Int`, `zerolog.Str`, …) — no `zap.Any`/`slog.Any` reflection,
  no key pools, no sorting, `DeterministicOrder` option deleted.
- The zerolog bridge may alternatively reuse the record's encoded bytes
  when the sink opts in; start with the plain loop, measure first.
- Expected: adapter medium writes roughly halve (slog 1,699 → ~900,
  zap 858 → ~450 ns); the first-party JSON sink (~300–400 ns for 12
  fields) becomes the advertised default path.

### W9 — Field dedupe (S)

`PERFORMANCE_PLAN` item E, now concrete:

- `op.*` is the canonical lifecycle namespace (domain, name, id, source,
  attempt, max_attempts, code, outcome, duration_ms).
- HTTP keeps `http.method/path/route/status`; `op.code` is emitted only
  for non-HTTP domains.
- Worker drops the `job.*` mirrors of `op.*` fields (mapping table in the
  migration guide: `job.name` → `op.name`, …). Dashboards update once,
  documented.

### W10 — Optional: general-purpose mini logger (M) — decide after W1–W8 measure

A trimmed fork surface (`hc/log`): `Debug/Info/Warn/Error` + the same
writer and encoder, for ordinary non-request log lines. Makes happycontext
a one-stop logger. Defer the decision until the core lands; the encoder
fork makes this cheap, but scope discipline says measure first.

## 3a. Write-ahead logging (WAL)

WAL applies to happycontext at three levels. The first is the core
design restated; the other two solve different problems. None touch disk.

### The request itself is the WAL (in-memory, per request)

This is the v2 core, framed precisely: the Event **is** a per-request,
in-memory, append-only write-ahead log.

- Every `hc.Add` (and `Error`, `SetMessage`, `SetRoute`) appends one
  typed record to a pooled, request-confined buffer. No map, no lock, no
  boxing — an append plus, for duplicate keys, a short linear scan so
  last-write-wins matches v0 semantics. Steady-state cost approaches a
  slice append (~10 ns, 0 allocs vs ~30–80 ns for today's map insert
  under the mutex).
- `op.End` is the **commit point**: one pass encodes the WAL into the
  final JSON line, writes it, and recycles the buffer to the pool. No
  snapshot, no clone — the WAL is the single representation.
- The WAL is always inspectable mid-request. The watchdog (W12) reads
  the WAL tail directly — an in-flight record is just "the log so far,"
  which is what makes OOM-killed and hung requests visible.
- Optional **black-box timeline**: because records are ordered and their
  positions are implicit sequence numbers, a policy can emit the WAL
  itself as a `steps` array (`{"seq":3,"t_ms":412.1,"k":"db.query"}`)
  for slow or failed requests. Zero cost when unarmed — the array is the
  records we already store; per-record timestamps (~32 ns each, one
  `time.Now()`) are only captured once the policy arms (on stall or on
  completion above a duration threshold).
- Bounds: pooled buffers recycle only up to a cap (mirroring the
  adapters' 160-entry pools today); oversized requests keep correctness
  by growing, and the watchdog path can mark `wal.truncated` instead.

This framing makes W3 and W12 the same structure with two read patterns:
commit at `End`, snapshot for the watchdog.

### How it works, end to end

The record — one per `Add`, ~64 B, no data copied (Go string headers
only; the `any` slot is used only for non-scalar values):

```go
type record struct {
    key  string  // field name — header copy, no bytes copied
    kind uint8   // string | int | float | bool | time | dur | err | raw | any
    num  int64   // ints, bools, unix-ms, durations
    flt  float64 // floats
    str  string  // string values
    any  any     // fallback: errors, raw JSON, user structs (rare)
}
```

Walkthrough of one request:

```
hc.Start(ctx, rt, start)
  pool.Get() → records[:0]; append header records (op.domain, op.name,
  op.id, …); activeSet.register(slot); ctx carries *Event.

hc.Add(ctx, "user_id", "u_8472")           ~10 ns, 0 allocs
  type-switch value → append typed record. Duplicate key overwrites the
  existing record in place (short scan; keeps v0 last-wins semantics and
  the log deduplicated so the encoder stays a single forward pass).

hc.Error(ctx, err) / SetMessage / SetLevel
  error stored as a record; message/level live in the scalar header.

[watchdog, background]  armed when elapsed > InflightThreshold
  emit operation_stalled with the WAL tail; switch the event to guarded
  mode so subsequent appends also record t_ms (see below).

op.End(&err)                                the COMMIT POINT
  resolve outcome: panic > error > explicit > 5xx > success.
  append completion records: duration_ms, op.code, op.outcome.
  level ← compiled policy merged with the requested floor.
  sampler(SampleInput) — scalars + Lookup only, no lock held:
    drop → activeSet.release(slot); pool.Put(event); return  (≤ 100 ns)
  keep  → sink.Write(rec):
            Record wraps the buffer zero-copy (read-only view).
            JSONSink: encode records in one pass (SWAR escaping),
                      single Write per event.
            bridges: iterate records → native typed fields, in order.
          activeSet.release(slot); pool.Put(event).
          pooled buffers recycle only ≤ cap; oversized requests keep
          their allocation — correctness first, the pool just refuses.
```

Rules of the log:

- **Append-mostly.** One writer (the request goroutine). No lock anywhere
  on the fast path; the only mutation beyond append is the rare
  duplicate-key overwrite.
- **Insertion order is emission order.** Deterministic output is
  structural — no sorting machinery anywhere downstream.
- **Exactly two readers.** The commit encoder at `End`, and the watchdog
  snapshot — and never concurrently with appends: arming flips the event
  to guarded mode (a per-event mutex consulted only by armed events and
  the watchdog; the fast path never touches it, and armed events are by
  definition slow, so the cost is irrelevant).
- **Bounded recycling.** Pool takes buffers back only up to a cap
  (records and encode bytes); bigger requests simply don't recycle.

Timeline arming, over the life of one slow request:

```
t=0      Start. No timestamps recorded — zero per-Add cost.
t=2.0 s  duration crosses SlowThreshold (or watchdog stall fires):
         timeline ARMED. Each Add now also appends t_ms (~32 ns each).
t=3.8 s  End → commit. steps: earlier records carry seq only (seq is
         the array position — always known); records since arming carry
         seq + t_ms. The tail is where the slowness lives, and the tail
         is what got timestamped.
```

Failure modes, exhaustively:

| Scenario | What the WAL does |
| --- | --- |
| Panic in handler | `End` recovers, appends panic + error records, commits with `op.outcome:"panic"`, re-panics |
| OOM-kill / SIGKILL mid-request | Commit never runs — the process is gone. Your trace is whatever the watchdog already emitted (`operation_stalled`), or the eager `operation_started` record if the domain's policy enables it |
| Sink blocked / slow pipe | Sync mode: the request blocks (v0 behavior). Buffered mode (W11): the pre-encoded line appends to the ring and the request returns immediately |
| Sampler drops the event | No completion records, nothing encoded, buffer recycled — the entire dropped path is ≤ 100 ns |
| Same key added twice | Last write wins — overwrite in place, v0 semantics |
| Request outgrows the cap | Keeps growing (correctness); buffer does not return to the pool |
| A second goroutine calls `hc.Add` | Request-confined contract: allowed only if the caller synchronizes (or the event is in guarded mode, which serializes appends) |

### W11 — Buffered sink: WAL-style async drain (S/M)

Problem: `Sink.Write` is synchronous on the request goroutine. A blocked
or slow destination (full stdout pipe, stalled network exporter) directly
extends request latency today.

Design (opt-in `hc.BufferedSink` wrapping any sink or writer):

- Finalized records are pre-encoded (W4/W8 make the line available without
  re-encoding) and appended to a bounded ring buffer — a memcpy plus index
  bump. The request path never blocks and never pays encoding twice.
- One background drainer concatenates buffered lines and issues a single
  `Write` per batch (`net.Buffers` writev where the writer supports it).
- Backpressure: when the ring is full, drop-oldest and increment a
  dropped-events counter (exposed via a stats hook). Never block, never
  allocate into unbounded memory.
- `Flush(timeout)` plus flush-on-signal helpers for graceful shutdown.
- Crash window: buffered-but-undrained lines are lost on hard exit — the
  same trade every async logger makes (zerolog diode, zap
  `BufferedWriteSyncer`). Documented, and the reason W12 exists.

Expected effect: with buffering on, the sink call on the request path
drops to ~100 ns regardless of destination health.

### W12 — In-flight records: literal write-ahead semantics (M) — data-gated

Problem: the event model writes at `End`. Requests that never end —
OOM-kill, SIGKILL, deadlock, runaway loop — leave zero trace. The worst
requests are the invisible ones; that is the strongest form of "your logs
are lying."

Design: the per-request WAL (§3a) is the backing store. Active
operations register in a small pool-slot set (one index append); the
watchdog emits `operation_stalled` by reading the WAL tail — current
fields, in order, plus the optional timeline once armed. Two arming
designs, cheapest first:

1. **Watchdog flush (recommended first).** Active operations register in a
   small pool-slot set (one index append, off the hot path). A background
   timer emits `operation_stalled` for any operation exceeding
   `InflightThreshold` (off by default; e.g. 10s), carrying the WAL
   contents — fields in append order, timeline if armed. Zero
   steady-state cost, catches hangs and kills.
2. **Eager start records.** Emit `operation_started` at begin. Doubles
   record volume, so it is only viable per-domain via policy
   (`InflightRecord: true`) for low-QPS, high-stakes domains.

Correlation is `op.id`; end records supersede start/stall records in
consumers. Both designs are additive — no change to the completion path.

### Rejected: durable disk-backed WAL

Append-only file, fsync policy, rotation, permissions, disk-full behavior
— that is log-shipper/agent territory (Vector, Fluent Bit), not a Go
logging library. It would dwarf the rest of v2 in maintenance. The
buffered sink's crash-loss window stays documented instead; users needing
durability run a shipper. Revisit only with concrete user demand.

## 3b. Encoding and runtime tricks inventory

Everything below was measured on this M4 under Go 1.27.0 or sourced from
the library's own documentation; each item is adopt / evaluate / reject
with the reason.

**Adopt**

- **Reuse the start-time reading for the event timestamp.** The Event
  already captures `time.Now()` at start; the emitted `time` field uses
  that reading, so completion needs zero extra clock calls. `time.Now()`
  costs ~32 ns on this machine — the second read per event is pure waste.
- **Integer epoch-ms as the default time field; RFC3339 strings opt-in.**
  Measured: epoch-ms total ~35 ns (and ~3 ns marginal with the reused
  reading), RFC3339Nano with `Now` ~60 ns. phuslu gets strings fast with
  a hand-written formatter; not worth it when integers are cheaper and
  what zerolog users already ingest.
- **Opt-in coarse clock** (atomic updated by a background ticker):
  ~8 ns vs ~35 ns for a fresh epoch-ms stamp, at the cost of
  millisecond-stale timestamps. A knob for extreme hot paths only.
- **Precomputed event prefixes** (zerolog-style): constant byte slices
  for `{"level":"info",` etc. per level — no key encoding, one append.
- **`RawJSON` field kind**: let callers attach pre-encoded JSON without
  re-escaping; the escape scan (even SWAR) is skipped entirely for blobs
  the caller already encoded once.
- **Batch drain writes** (W11): concatenate buffered lines into one
  buffer and issue a single `Write`; use `net.Buffers` (writev) when the
  destination supports it. phuslu/log documents automatic writev as one
  of its biggest wins under load (up to ~10× in its benchmarks).
- **Encode-once, multi-sink reuse**: the Record carries a lazily built
  encoded-bytes cache; fan-out sinks and the zerolog bridge reuse it
  instead of re-encoding (also removes `maps.Clone`-style duplication at
  the sink layer).
- **Float formatting**: shortest round-trip via `strconv.AppendFloat(..,
  'f', -1, 64)` ≈ 25 ns — same as jsontext; `'g'` is ~35 ns. Ints ~6 ns.
  Nothing to build; just pick the right format flags.

**Evaluate**

- **True SIMD escaping (AVX2/NEON assembly, go:build-tagged)**: goccy and
  sonic do this on amd64. Pure-Go SWAR already captures 11–18% on the
  dominant payload; assembly would add amd64/arm64 split code paths for a
  slice of the remaining cost. Revisit only if post-v2 profiles show the
  escape scan above ~5% of lifecycle time.
- **`encoding/json/v2` as the `any` fallback**: Go 1.27 ships json/v2 as
  the default `encoding/json` backend (fast unmarshal, parity encode) —
  our interface-fallback marshaling gets faster with zero work. Nothing
  to adopt actively; benefit arrives with the toolchain bump.

**Reject**

- **sonic / goccy as dependencies**: we never marshal whole structs — the
  hot path is escape + itoa + framing, all owned by the fork. Measured on
  this M4 (Go 1.27.0) for the one place a marshaler could plausibly appear
  — the `any` fallback on a 12-field `map[string]any` event:

  | Encoder | small flat map | nested map |
  | --- | ---: | ---: |
  | stdlib `encoding/json` (1.27) | ~1,552 ns / 29 al | ~1,350 ns / 10 al |
  | goccy/go-json | ~793 ns / 2 al | ~1,643 ns / 7 al |
  | bytedance/sonic | ~1,070 ns / 3 al | ~1,900 ns / 7 al |
  | **typed fork append loop** | **~11 ns / 0 al** | n/a (fields are typed) |

  Two conclusions. First, goccy/sonic only win on flat maps and only by
  ~2×; on nested maps on arm64 both are slower than the Go 1.27 stdlib
  (now json/v2-backed), so the win is payload- and platform-dependent —
  exactly the kind of dependency not worth taking. Second, the table is
  the strongest argument for W3 itself: whole-event map marshaling is a
  1.3–2.1 µs tax; the typed append path is ~140× faster. The internal
  encoder interface still lets users plug goccy/sonic in themselves if
  they have struct-heavy `any` fields.
- **jsontext as the primary encoder**: ~45% slower than the fork on
  clean ASCII strings (7.1 vs 4.8 ns short, ~50 vs 31 ns at 96 chars),
  comparable only on escape-heavy/unicode payloads. Wrong side of the
  trade for the default path; right as the fallback backend.
- **Arenas (`GOEXPERIMENT=arenas`)**: still experimental, restricted
  semantics; pooling already gets us to near-zero steady-state allocs.

## 4. Targets

Gates for the v2 core PR, same machine and harness as §1:

| Benchmark | v0.4.0 | v2 target |
| --- | ---: | ---: |
| OperationLifecycle (kept) | ~615 ns / 14 allocs | ≤ 250 ns / ≤ 4 allocs (stretch 150 / 2) |
| OperationLifecycleDropped (8 fields) | ~628 ns / 9 allocs | ≤ 100 ns / ≤ 2 allocs |
| Add-only steady state | ~30 ns/field + boxing | ≤ 20 ns/field, 0 allocs |
| std middleware (discard sink) | ~1,000 ns / 26 allocs | ≤ 350 ns / ≤ 8 allocs |
| First-party JSON sink, 12 fields | n/a | ≤ 400 ns, ≤ 2 allocs |
| String escape, 96-char clean ASCII | ~31 ns (fork table) | ≤ 26 ns (hybrid SWAR) |
| Buffered sink append (opt-in, W11) | n/a | ≤ 100 ns, never blocks |
| Bridge medium write (slog/zap/zerolog) | 1,699 / 858 / 354 ns | ~900 / ~450 / ~300 ns |
| Disabled-level adapter writes | ~3 ns | unchanged |

Quality gates (carried from `PERFORMANCE_PLAN.md` plus additions):

- Benchmarks at 0/8/32/128 fields; sampling rates 0/0.05/0.5/1; policies
  0/1/16/128; enabled and disabled adapter levels; serial and parallel.
- Golden-output equivalence: v2 first-party sink output matches v1
  zerolog-adapter output for the same logical event (modulo field order,
  which becomes insertion-ordered).
- Encoder fuzzing (1M execs in CI), all-module tests + `-race`, and the
  failure/panic/cancellation/timeout retention matrix.

## 5. Release strategy

Decision needed: the literal version number.

- **Recommend: ship as v1.0.0.** The project has never shipped 1.0, so the
  breaking redesign is the natural first stable release. No module path
  changes; none of the 12 nested modules need `/v2` treatment; users keep
  their import paths. "v2" can remain the architecture's codename in docs.
- Alternative: literal v2.0.0. Go modules then require `github.com/
  happytoolin/happycontext/v2` paths, and every nested module moves to
  `adapter/slog/v2` directories with matching tags — real churn for users
  and the release tooling, with no technical gain over v1.0.0.

Either way: v0.5.0 ships W1 + W2 (first-party sink, non-breaking) first,
so the encoder gets production soak time before the break.

Migration support:

- A `MIGRATION.md` with a before/after table for every removed or changed
  symbol (BeginOperation/FinishOperation/OperationFinish → internal,
  `op.End(cfg, &err)` → `op.End(&err)`, `SampleInput.Event` →
  `Lookup`, `Sink.Write(map)` → `Sink.Write(*Record)`,
  `DeterministicOrder` → deleted, `job.*` → `op.*`, explicit-outcome
  precedence change).
- One release with `Deprecated` annotations on removed APIs where
  mechanically possible (the Sink interface change cannot be softened;
  bridges are separate modules, so old adapters pin to v0 core).
- Outcome precedence fix (`PERFORMANCE_PLAN` reserved item): panic >
  error > explicit outcome > status ≥ 500 > success — lands in v2 with a
  changelog callout.

## 6. Sequence

| Phase | Contents | Breaking? |
| --- | --- | --- |
| v0.5.0 | W1 encoder fork, W2 `hc.NewJSONSink`, remove adapter sort machinery (see below), remaining small cuts | soft¹ |
| v2 core PR | W3 typed Event, W4 Record/Sink, W5 Compile, W6 sampler, W7 lifecycle, in one coordinated break | yes |
| v2 follow-ups | W8 bridges, W9 field dedupe, W11 buffered sink, migration guide, benchmarks refresh | yes (modules) |
| later | W10 mini logger, W12 in-flight records, console/pretty output, decided by data | no |

¹ v0.5.0 deletes `SinkOptions.DeterministicOrder` and the pooled-key
sort from all three adapters. Default output bytes do not change — the
option is off by default; only opt-in users are affected and they get a
compile error, not silent drift. The changelog must be honest about
where the capability went: **nowhere — destination loggers cannot take
it over** (none of zap/slog/zerolog offers sorting, and the
`map[string]any` sink contract has already erased insertion order by
the time a host logger sees anything). Migration for stable-byte
assertions: `hc.TestSink` (order-free map comparison) or pin the v0.4
adapter modules, which keep working against the new core because the
`Sink` interface is unchanged in 0.x. This removes ~90 LOC replicated
across three modules and kills the deterministic-order benchmark
surface a release early. Landed as PR #21.

**v0.5 cleanup candidates** (usage-traced against the whole repo; each
is a compile-visible removal, same policy as the sort):

- `hc.Commit(ctx, sink, level)` — only caller in the repo is an
  internal benchmark; the per-call sink parameter contradicts every
  other API (Config carries the sink). Removal deletes
  `happycontext.go` outright.
- `hc.GetLevel(ctx)` — read-back of a value the caller just set; used
  only by the copy-pasted demo line in 8 example programs
  (`cmd/examples` is an unpublished module, so fixing them is free).
- `LevelRank`, `MergeLevelWithFloor` — internal machinery with exactly
  one internal caller each; un-export.
- `EventMessage`, `EventHasMessage`, `EventStartTime` — zero internal
  callers; superseded direction-wise by the v2 `Lookup` sampler. Can
  ride with the batch or wait for W6 — both defensible.

Keep deliberately (verified, not smells): `NewContext`/`FromContext`
(17 internal uses, genuinely useful), `IsValidLevel`/`IsValidOutcome`
(public validation, internally used), `TestSink` (now the documented
migration target for stable-byte assertions), the `httpsnoop`
dependency in `integration/std` (hand-rolling its ResponseWriter
interface fidelity would re-implement it worse), and
`BeginOperation`/`FinishOperation`/`hydrateOperationStart` (load-bearing
integration plumbing — their removal **is** W7, not a cleanup).

**Repo hygiene found in the v0.5 sweep** (tools: gofmt/gofumpt,
`staticcheck -checks U1000,S*` at latest, go vet, git archaeology):

- `prebox_test.go` fails gofmt/gofumpt (map-literal alignment) — the
  only formatting violation in 12 modules. Fix it, then add a format
  gate to CI.
- CI gaps: tests + vet loop all modules (good) but there is **no
  `-race` mode and no gofmt gate** in CI, despite the Justfile
  advertising lint tooling. Given the concurrency-test investment,
  `go test -race` in CI is the highest-value addition.
- `Justfile` `tidy` recipe only processes the root module, while
  `test`/`bench` loop every module — modules silently miss
  `go fix`/`go mod tidy`.
- `go` directive drift across the 12 go.mod files (`1.24`, `1.24.0`,
  `1.25.0`) — harmonize.
- Stale release tooling: the tag backfill is complete (`v0.0.1`+
  new-format tags exist), so `scripts/backfill-go-tags.sh`, its README
  paragraph, and arguably the three leftover `happycontext-v*` tags
  are removable. The two lockstep-release scripts are wired into
  `release.yml` — keep those.
- `cmd/examples` carries one 8-line `GetLevel` demo block copy-pasted
  across 9 programs; removing `hc.GetLevel` (candidate above) deletes
  ~70 lines of duplication with it.
- `BENCHMARK_REPORT.md` references the deterministic benchmark case
  removed by PR #21 — historical snapshot; optionally annotate.
- Clean bills of health: zero unused unexported symbols anywhere
  (staticcheck U1000), zero simplification findings, no TODO/FIXME
  debt, no duplicated test helpers in `benches`, `.bench/` properly
  ignored, fiber v2/v3 middleware similarity is inherent to
  supporting two majors (codegen dedup already rejected).

W8 depends on W4's Record type; W3+W4+W5+W6+W7 land together because the
storage change cuts across all of them. Every phase keeps the full test
matrix green and carries before/after benchstat output in the PR.

## 7. Sample v2 API (working names)

Everything below follows from W2–W9. Names are working names; the
contracts are the point.

### HTTP service — first-party sink, zero logger deps

```go
func main() {
	sink := hc.NewJSONSink(os.Stdout) // W2: forked encoder, one write per event

	rt, err := hc.Compile(hc.Config{ // W5 · compile once, fail fast
		Sink:         sink,
		SamplingRate: 0.05, // healthy traffic at 5%; errors always kept
		Timeline:     hc.TimelineOnSlow(2 * time.Second), // black-box steps
	})
	// literal configs: rt := hc.MustCompile(cfg) — the regexp idiom
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		hc.Add(r.Context(), "user_id", "u_8472", "feature", "checkout")
		hc.AddRawJSON(r.Context(), "cart", preEncodedCartJSON) // W1: skip re-escaping

		if err := charge(r.Context()); err != nil {
			hc.SetMessage(r.Context(), "checkout_failed")
			hc.Error(r.Context(), err)
			http.Error(w, "internal error", 500)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	_ = http.ListenAndServe(":8080", stdhc.Middleware(rt)(mux))
}
```

### Background job — the request-WAL lifecycle

```go
func runJob(ctx context.Context, rt *hc.Runtime, meta workerhc.JobMeta) (err error) {
	op := hc.Start(ctx, rt, hc.OperationStart{ // W7: one lifecycle
		Domain: hc.DomainJob,
		Name:   "billing.reconcile",
		ID:     meta.ID,
		Source: meta.Queue,
	})
	defer op.End(&err) // commit: encode WAL, write, recycle buffer (§3a)

	hc.Add(op.Context(), "tenant", "enterprise")
	return reconcile(ctx)
}
```

### Sampling — scalars plus field lookup, no Event access

```go
rt, _ := hc.Compile(hc.Config{
	Sink: sink,
	Sampler: hc.ChainSampler(
		hc.RateSampler(0.05),
		hc.KeepErrors(),
		hc.KeepSlowerThan(500*time.Millisecond),
		func(next hc.Sampler) hc.Sampler { // W6: middleware, Lookup instead of Event
			return func(in hc.SampleInput) bool {
				if tier, ok := in.Lookup("user_tier"); ok && tier == "enterprise" {
					return true
				}
				return next(in)
			}
		},
	),
})
```

### Sinks — Record view; bridges and custom sinks

```go
type Sink interface { // W4 · slog.Handler.Handle shape
	Write(ctx context.Context, rec *Record) // read-only, valid during Write; concurrency-safe
}

type Record struct{ /* ... */ }
func (r *Record) Level() Level
func (r *Record) Message() string
func (r *Record) Fields() []Field // insertion-ordered, zero copy
func (r *Record) Lookup(key string) (any, bool)
func (r *Record) Encoded() []byte // W8: encode-once cache, reuse across sinks

// official bridges (unchanged constructor shape):
sloghc.New(logger)     // slog.Logger
zaphc.New(logger)      // *zap.Logger
zerologhc.New(&logger) // *zerolog.Logger — may reuse rec.Encoded() directly

// custom sink example:
type redisSink struct{ rdb *redis.Client }
func (s redisSink) Write(ctx context.Context, rec *hc.Record) {
	f := append([]byte(nil), rec.Encoded()...) // copy: bytes are recycled after Write
	s.rdb.RPush(ctx, "events", f)
}
```

### Buffered mode (W11) and graceful shutdown

```go
buf := hc.NewBufferedSink(sink,
	hc.WithBufferCapacity(4096),           // events; drop-oldest when full
	hc.WithDroppedHook(func(n uint64) { metrics.LogsDropped.Add(n) }),
)
rt, _ := hc.Compile(hc.Config{Sink: buf, SamplingRate: 1})

defer buf.Flush(2 * time.Second) // drain at shutdown; never blocks requests
```

### Emitted event (timeline armed on a slow failure)

```json
{"level":"error","time":"2026-08-30T14:03:12Z","message":"checkout_failed",
 "op.domain":"http","op.name":"GET /orders/{id}","http.method":"GET",
 "http.path":"/orders/8472","http.status":500,"duration_ms":1837,
 "op.outcome":"failure","user_id":"u_8472",
 "error":{"message":"charge declined","type":"*app.ChargeError"},
 "steps":[{"seq":1,"k":"user_id"},{"seq":2,"k":"cart"},
          {"seq":3,"t_ms":1836.9,"k":"stripe.charge"}]}
```

### v0 → v2 migration map

| v0 | v2 |
| --- | --- |
| `stdhc.Middleware(hc.Config{...})` | `rt, err := hc.Compile(cfg)`; `stdhc.Middleware(rt)` |
| `hc.StartOperation(ctx, start)` + `op.End(cfg, &err)` | `hc.Start(ctx, rt, start)` + `op.End(&err)` |
| `hc.BeginOperation` / `FinishOperation` / `OperationFinish` | internal; gone from the public API |
| `hc.EventFields(in.Event)["k"]` in samplers | `in.Lookup("k")` |
| `Sink.Write(level, msg, fields map[string]any)` | `Sink.Write(rec *Record)` |
| `NewWithOptions(l, SinkOptions{DeterministicOrder: true})` | deleted — insertion order is structural |
| worker `job.*` mirror fields | canonical `op.*` only (mapping table in guide) |
| explicit outcome with error (`OutcomeSuccess` + err) | error wins — precedence: panic > error > explicit > 5xx > success |

## 8. Explicitly rejected

- Pre-encoded JSON buffer as the Event store — breaks bridge/sampler field
  access or forces dual storage; typed slice keeps one representation.
- Forking zap or phuslu/log — larger core / no output continuity (see §2).
- sonic / goccy as dependencies, and assembly SIMD escaping — we never
  marshal whole structs; pure-Go SWAR captures most of the win with none
  of the platform cost (see §3b).
- jsontext as the primary encoder — ~45% slower on the dominant payload;
  stays the fallback backend (see §3b).
- A cache, a new configuration framework, or any new runtime dependency —
  restated from `PERFORMANCE_PLAN.md`; still true.
- Durable disk-backed WAL — shipper territory; see §3a.
- Generics-heavy typed-field public API (`Add(ctx, Field(...))`) — keeps
  `any` ergonomics; the type switch lives inside the core.
- Cross-module code generation for adapters — rejected previously; the
  bridge rewrite makes the shared-shape problem smaller anyway.

## 9. FAQ

**Why a slice instead of a map?** Writes are appends; random access was
only needed by the sink clone and samplers, which read a handful of
fields. The map costs hashing, boxing, growth, a clone per write, and
random iteration order. The slice append is ~10 ns, ordered, zero-copy.
`Lookup` covers the rare read — scanning a dozen records beats hashing a
string key.

**Can I share an Event across goroutines?** Not on the fast path —
Events are request-confined: one writer, no lock, by contract. v0's
internal mutex made every request pay for a rare sharing pattern.
Synchronize externally if you must fan out. Armed (stalled) events are
the internal exception — guarded mode, and that path is slow by
definition.

**Why change the Sink interface?** The v0 contract forces a clone per
kept event and reflection-based conversion in every adapter. The v2
`Record` is read-only, ordered, typed, zero-copy — and `rec.Encoded()`
hands finished bytes to sinks that want nothing else. Migration is one
signature; the guide shows a before/after.

**Why fork zerolog instead of importing it?** We need ~800 LOC of
append-only encoder, not a logger API. Vendoring inverts that
dependency to exactly the code we use, with attribution and property
tests; the internal interface plus a `jsontext` backend is the exit
ramp.

**Isn't drop-oldest lossy?** Deliberately. The alternatives when the
ring is full are blocking requests or unbounded memory. Dropped counts
surface via hook; `Flush` drains at shutdown.

**Does insertion order leak internals?** It exposes the order fields
were attached — deterministic per code path, stable across runs. That
replaces v0's random map order papered over with an optional sort.

**Why is a dropped event ≤ 100 ns, not zero?** Sampler decision, slot
release, and pool return remain. The other ~500 ns of v0's dropped path
(completion appends, snapshot) simply never execute.

**Why does field order matter at all? Why did v0 sort?** Three real
needs: tests (golden/snapshot tests need stable bytes — map-random order
forces field-by-field parsing instead of a string compare), incident
diffing (stable order turns a two-event comparison into a clean field
diff), and downstream compression (similar lines compress better when
their byte layout is stable). v0 needed the machinery because Go
randomizes map iteration order on purpose, so a map-walk encode emits a
different order every event; the pooled-key sort replicated in each
adapter (+380 ns / +22% on slog medium writes) was the only fix, and it
hid behind an option because it cost speed. v2 makes the choice
disappear: insertion order is the byte order — determinism is free, the
option is deleted, and the order is more meaningful than alphabetical
(header fields, user fields in attach order, completion last).

**Why not let each adapter sort if its user wants it?** Someone must
still pick an order when bytes get emitted — if the core doesn't, every
sink sorts independently: the same machinery replicated per adapter
(v0's exact situation), inconsistent output across sinks for the same
event, and custom-sink authors each reinventing it. Fixing order at
creation is the only placement where it costs zero. Consumers remain
free to reorder for display (zerolog's ConsoleWriter does exactly
this), and the rare user who wants alphabetical bytes can wrap their
sink in ~10 lines that sort `rec.Fields()` before delegating — a
consumer concern, not core machinery.

**Then why keep the sort at all before v2?** We don't. v0.5.0 removes
`DeterministicOrder` and its machinery from all three adapters a
release early (§6, footnote 1): default bytes are unchanged, opt-in
users get a loud compile error, and stable-byte tests migrate to
`hc.TestSink` or pin the v0.4 adapters. The one migration claim we must
not make is "your logger can sort it" — no host logger offers sorting,
and the map contract has already thrown the order away.

**What happens to dashboards?** One field rename set: worker `job.*`
mirrors collapse into `op.*`, `op.code` goes non-HTTP-only. Mapping
table plus dual-write window during the preview.

## 10. Glossary

- **WAL / Event** — the per-request, in-memory, append-only log of typed
  records. One writer, two readers (commit, watchdog).
- **record (storage)** — one log entry: key + kind + typed value slots,
  ~64 B. Not the public `Record`.
- **Record (public)** — the read-only view handed to a sink during
  `Write`: level, message, ordered fields, `Lookup`, `Encoded()`.
- **Commit** — `op.End`: outcome, completion records, sampling, encode,
  single write, pool return.
- **Runtime** — the compiled immutable result of `hc.Compile(Config)`;
  built once, shared by all requests.
- **Bridge** — adapter module consuming the Record's typed fields into a
  host logger (slog/zap/zerolog).
- **First-party sink** — `hc.NewJSONSink(w)`, the built-in encoder; no
  third-party logger required.
- **Active-set** — registry of in-flight operations the watchdog scans.
- **Guarded mode** — armed events serialize appends behind a mutex so
  the watchdog can snapshot; the fast path never touches it.
- **Timeline arming** — recording `t_ms` per record once a duration
  threshold is crossed; sequence is always implicit.
- **Request-confined** — ownership contract: one goroutine per Event,
  no internal locking.
- **Drop-oldest** — buffered-sink backpressure: discard the oldest line
  (counted) rather than block producers.
- **RawJSON** — field kind holding pre-encoded JSON appended verbatim;
  escape scan skipped.

## 11. Prior art: wide-event and canonical-log-line libraries

Research pass 2026-08-31 (sources cloned and read at file:line level;
beeline-go micro-benchmarks measured locally on the M4, everything else
as claimed by the projects). Question: what public libraries implement
the "one wide event per request" model, and how do their performance,
error handling, and sampling compare to ours?

### The primary sources

- **Stripe — canonical log lines (internal, blog-published).** One line
  per request per service, emitted in a Ruby `ensure` block (fires even
  while the middleware stack unwinds on an exception) with the logging
  statement itself wrapped in a nested `rescue` — building a canonical
  line can never fail a request. Field names are a formal **protobuf
  contract**; schema changes are breaking changes. Lines ship
  asynchronously Kafka → S3 → Presto/Redshift and power the Developer
  Dashboard and incident queries — logs in place of a metrics pipeline.
  (stripe.com/blog/canonical-log-lines)
- **Honeycomb — "wide events."** Coined the term; their archived
  **beeline** SDKs (Go/Node/Python/Ruby/Java) are the closest public
  analog to hc: middleware opens a request span, fields accumulate,
  one enriched event per request. Sun-setted in favor of OTel, but the
  design DNA is ours.

### The public libraries, compared

| Library | Language | Field accumulation | Performance | Error capture | Sampling |
| --- | --- | --- | --- | --- | --- |
| beeline-go (archived 2025-08) | Go | ctx span + mutexed map; map copied at send | **measured, M4**: `AddField` 39–49 ns; `CreateSpan` 488 ns / 7 al; `SendSpan` 345 ns / 8 al; async batched transport (50/batch, 100 ms, 10k queue, drop-on-overflow) | 5xx → status field; **no `recover()`** — panics propagate, event sent sans status via deferred Send; drops are silent | SHA-1(traceID) deterministic whole-trace; field-based hook; no salt |
| pino-http | Node | pino child-logger bindings — **no mutable event** | their stale autocannon (2013 MBP): 21.5k req/s vs 46.1k no-logger | real err on socket errors; 5xx → **synthetic** Error, no stack; aborted-request path known-broken (own skipped test) | none (level-silent trick + `ignore` predicate) |
| evlog (HugoRCD; audited §7) | TS | closure over one mutable object, in-place merge, stringify once at emit | their published suite: ~2.7 µs/req lifecycle; `emit` 400 ns; wide-event 7.7× pino | full error objects + rethrow; drain failures swallowed; **circular values can crash emit's stringify** | head Math.random per level (errors 100%) + tail keep-rules; not deterministic |
| lograge | Ruby | ActiveSupport notifications → one KV line | no benchmarks; sync string building | exception → status via ExceptionWrapper; **no rescue around its own emission** — formatter bug breaks requests | none (binary include/exclude) |
| Serilog.AspNetCore `UseSerilogRequestLogging` | C# | middleware + DiagnosticContext collector | qualitative claim only ("fewer events constructed/transmitted/stored") | exceptions & ≥500 → Error; **4xx stays Information**; logger exceptions propagate | none |

### What it says about our design

1. **The 12-field gate (293 ns / 0 al) is a different class.** It beats
   beeline-go's span *creation* alone (488 ns / 7 al) before any send;
   nobody else has pooled-buffer + single-`Write` on the hot path.
2. **Error-bypass-sampling is unique.** Zero OSS implementations have
   "always emit errors, sample successes" (amendment 4 is a real
   differentiator). The only kindred philosophy is Stripe's
   ensure+rescue: observability must be most reliable when things are
   worst.
3. **Sealing (amendment 20) is validated** — evlog's post-emit warning
   path is exactly the use-after-emit bug class sealing prevents.
4. **BufferedSink (W11) would be a library-level first.** Every OSS
   implementation punts buffering to downstream infrastructure (Kafka,
   Serilog.Sinks.Async, SonicBoom); Stripe does it in infra, not the
   library.
5. **Worth stealing: schema discipline.** Stripe's protobuf field
   contract built org-wide "muscle memory" for querying logs. A short
   README policy note — `op.*`/`http.*` names are stable, renames are
   breaking — is the cheap version of that.

Sources: github.com/honeycombio/beeline-go@cb95e4b, github.com/pinojs/
pino-http, github.com/HugoRCD/evlog, github.com/roidrage/lograge,
github.com/serilog/serilog-aspnetcore, stripe.com/blog/canonical-log-
lines, docs.honeycomb.io. Full research notes with file:line citations
in the PR that added this section.
