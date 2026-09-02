# happycontext v2 — Final Design

Status: **locked** (2026-08-30). This is the buildable specification.
Rationale, measurements, and the research trail live in `V2_PLAN.md`;
this document only states what we build and how it is judged. v0.5.0
already shipped the adapter sort removal (PR #21) — that decision is
behind us.

## 1. The path

| Release | Contents | Breaking | Gate |
| --- | --- | --- | --- |
| **v0.5.0** — shipped | adapter sort removal, `just tidy` all-modules | soft (done) | — |
| **v0.6.0** | W1: `internal/hcjson` encoder fork (SWAR hybrid, zerolog-table property test, fuzz 1M, golden vs current zerolog adapter) · W2: `hc.NewJSONSink(io.Writer)` on the *current* Sink interface · jsontext fallback backend behind the internal encoder interface | none | golden output ≡ zerolog adapter (modulo ordering); fuzz clean; soak in `benches` |
| **v1.0.0** — the break (S-series) | W3 typed Event (request-WAL) · W4 `Record`/`Sink` · W5 `Compile`/`Runtime` · W6 sampler `Lookup` · W7 single lifecycle · W8 bridges on `[]Field` · W9 field dedupe · `MIGRATION.md` | yes, lockstep (root + all nested modules) | performance gates §4; full matrix green under `-race`; migration guide published *before* merge |
| **v1.1.0** | W11 `BufferedSink` (async drain, writev batches) · W12 stall watchdog + in-flight records + timeline arming · encode-once multi-sink reuse | none | buffered append ≤ 100 ns; watchdog emits within 1 s of threshold under load |
| **later, data-gated** | W10 `hc/log` mini logger · console writer | none | only if post-1.0 profiles justify |

The v0.6 encoder exists so the highest-risk component earns production
trust on the old core before anything breaks. Nothing else ships
between v0.6 and 1.0.0 — the break lands as the coordinated S-series on
v2 (§9) and releases only when complete.

## 2. The public API

### Construction — fail fast, once

```go
func Compile(cfg Config) (*Runtime, error)
func MustCompile(cfg Config) *Runtime // regexp idiom: literal configs panic at startup

type Config struct {
    Sink              Sink                // required to emit
    SamplingRate      float64             // healthy traffic, [0,1]
    LevelSamplingRates map[Level]float64
    Sampler           Sampler             // overrides built-in sampling
    OperationPolicies map[Domain]OperationPolicy
    Message           string              // default per domain
    // Timeline and StallWatchdog join Config in v1.1 when the features
    // ship; v1.0 omits them rather than carrying dead zero-value fields.
}

type Runtime struct{ /* immutable; shared by all requests */ }
```

`*Runtime` replaces `Config` in every integration constructor. Bad
config is a construction-time error, never a per-request clamp. A nil
`Sink` remains a valid no-op runtime (requests run, nothing emits) —
the v0 zero-config behavior, kept deliberately.

Errors follow std conventions: sentinel values wrapped with `%w`,
`fmt.Errorf("hc: sampling rate %g: %w", …)` phrasing, `errors.Is`
works. v1.0 ships at least `ErrInvalidRate`, `ErrInvalidLevel`,
`ErrInvalidOutcome`.

### Levels — slog-style

```go
type Level int // String() renders "DEBUG" | "INFO" | "WARN" | "ERROR"
const (
    LevelDebug Level = -4 // slog-compatible ranks
    LevelInfo  Level = 0
    LevelWarn  Level = 4
    LevelError Level = 8
)
```

Every major logger (slog, zap, zerolog) uses int levels; the wire
format is unchanged — sinks serialize the same strings they emit today.

### The request WAL — write API

```go
func Start(ctx context.Context, rt *Runtime, start OperationStart) *Operation

type OperationStart struct {
    Domain      Domain // http | job | msg | cli | custom
    Name, ID, Source string
    Attempt, MaxAttempts int
}

func (op *Operation) End(errp *error) bool // deferred; captures panic, re-panics
func (op *Operation) Context() context.Context

// usable anywhere the enriched context flows; these return nothing —
// with no event attached they are silent no-ops, like slog helpers:
func Add(ctx context.Context, key string, value any, kv ...any)
func AddRawJSON(ctx context.Context, key string, raw []byte)
func Error(ctx context.Context, err error)
func SetMessage(ctx context.Context, msg string)
func SetRoute(ctx context.Context, route string)
func SetLevel(ctx context.Context, level Level)
```

`Level`, `Domain`, `Outcome`, `DefaultMessage`,
`DefaultOperationMessage` are unchanged from v0 — wire compatibility of
field *values* is preserved.

### Sinks — read API

`Record` and `Field` follow `slog.Record`/`slog.Value` conventions —
read-only views with `Kind()` and typed getters.

```go
type Sink interface {
    // slog.Handler.Handle shape: the operation's ctx at commit,
    // background from the watchdog and the drainer.
    // Implementations must be safe for concurrent use (amendment 2).
    Write(ctx context.Context, rec *Record)
}

type Record struct{ /* read-only view; valid only inside Write */ }
func (r *Record) Level() Level
func (r *Record) Message() string
func (r *Record) Fields() []Field // insertion order; zero copy
func (r *Record) Lookup(key string) (any, bool)
func (r *Record) Encoded() []byte // encode-once cache; reuse freely

type Field struct{ /* typed union */ }
func (f Field) Key() string
func (f Field) Kind() FieldKind // String|Int|Uint|Float|Bool|Time|Duration|Err|Raw|Any
func (f Field) Str() string
func (f Field) Int() int64
func (f Field) Float() float64
func (f Field) Bool() bool
func (f Field) Time() time.Time
func (f Field) Duration() time.Duration
func (f Field) Err() error
func (f Field) Raw() []byte
func (f Field) Any() any

// first-party (v0.6) and buffered (v1.1):
func NewJSONSink(w io.Writer, opts ...JSONSinkOption) *JSONSink
func NewBufferedSink(next Sink, opts ...BufferedOption) *BufferedSink
func WithBufferCapacity(events int) BufferedOption
func WithDroppedHook(fn func(n uint64)) BufferedOption
func (b *BufferedSink) Flush(ctx context.Context) error
```

### Sampling — scalars plus lookup

```go
type SampleInput struct {
    Domain Domain; Operation string; Outcome Outcome; Code int
    Method, Path string; StatusCode int
    Duration time.Duration; Level Level; HasError bool
}
func (in SampleInput) Lookup(key string) (any, bool)
func (in SampleInput) Fields() []Field // read-only; replaces v0 EventFields iteration

type Sampler func(SampleInput) bool
type SamplerMiddleware func(Sampler) Sampler
func ChainSampler(base Sampler, mws ...SamplerMiddleware) Sampler
func RateSampler(rate float64) Sampler
func AlwaysSampler() Sampler
func NeverSampler() Sampler
func KeepErrors() SamplerMiddleware
func KeepPathPrefix(prefixes ...string) SamplerMiddleware
func KeepSlowerThan(min time.Duration) SamplerMiddleware
```

Errors and panics always bypass rate sampling (unchanged).

### Testing

```go
func NewTestSink() *TestSink
func (t *TestSink) Events() []CapturedEvent
type CapturedEvent struct{ Level Level; Message string; /* Fields(), Lookup */ }
```

### Modules

- Bridges: `sloghc.New(*slog.Logger)`, `zaphc.New(*zap.Logger)`,
  `zerologhc.New(*zerolog.Logger)`. `NewWithOptions` and `SinkOptions`
  are **removed** — there is nothing to configure until proven
  otherwise.
- Integrations: `stdhc.Middleware(rt)`, same one-argument shape for
  gin/echo/fiber/fiberv3; `workerhc.Start(ctx, rt, meta)`.

### Removed from the public API (the complete list)

`BeginOperation`, `FinishOperation`, `OperationFinish`, `Commit`,
`GetLevel`, `LevelRank`, `MergeLevelWithFloor`, `EventFields`,
`EventMessage`, `EventHasMessage`, `EventHasError`, `EventStartTime`,
`NewContext`, `FromContext`, `NormalizeConfig` (superseded by
`Compile`). The in-flight WAL is written by the `hc.*` helpers and read
at `End` — nothing reads it mid-flight by design, so the context
accessors go internal.

## 3. The feel

1. **Two lines to adopt.** `Compile` + middleware. Every misconfiguration
   is an error at startup, in your face, once.
2. **`hc.Add` reads as annotation, not logging.** Handlers describe what
   the request *is*; the library decides what the line looks like, once,
   at the end.
3. **One event, always.** Header fields → handler fields in attach
   order → completion fields. Deterministic by construction, readable
   by humans, diffable in incidents.
4. **Zero dependencies by default.** The first-party sink needs no
   logger; bridges exist for shops that already run slog/zap/zerolog,
   with unchanged constructor ergonomics. The root module's only
   non-stdlib require is go.uber.org/goleak, a test-only dependency
   (goroutine-leak verification); production builds stay dependency-free.
5. **Failures are sacred.** Errors and panics are never sampled away.
   Sampling shapes healthy traffic; it never hides sick traffic.
6. **The worst requests are the most visible.** Slow requests grow a
   `steps` timeline; hung requests emit `operation_stalled` from the
   watchdog. The black box exists precisely for the flights that
   didn't land.
7. **Contracts are short and printed on the box.** The Record view is
   valid during `Write`; the WAL is request-confined; pooled buffers
   recycle under a cap. Three sentences, no fine print.
8. **Go-shaped.** Context-first parameters, no fluent chains on the hot
   path, options functions only where options earn their keep.

## 4. The performance — acceptance gates

Same machine and harness as the v0.4.0 baseline; every gate requires
benchstat evidence in the PR. No number counts without the property
test passing first (§05's cautionary tale).

| Gate | v0.4.0 | v1.0.0 requirement |
| --- | ---: | ---: |
| OperationLifecycle (kept) | ~615 ns / 14 al | **≤ 250 ns / ≤ 4 al** (stretch 150 / 2) |
| Dropped event (8 fields) | ~628 ns / 9 al | **End-drop path ≤ 100 ns / ≤ 2 al** (sampler + release + pool; field appends excluded — full dropped lifecycle benched separately, ≤ 300 ns) |
| Add steady state | boxing per field | **single pair ≤ 20 ns / 0 al** (constant values); multi-pair variadic and non-constant values may allocate the `[]any` — gate is stated per shape and benched with realistic (non-constant) values |
| std middleware (discard sink) | ~1,000 ns / 26 al | **≤ 350 ns / ≤ 8 al** |
| First-party sink, 12 fields | n/a | **≤ 400 ns / ≤ 2 al** |
| Escape, 96-char clean ASCII | ~31 ns | **≤ 26 ns** (hybrid SWAR, measured) |
| Bridges, 12 fields | 1,699 / 858 / 354 ns | **≤ 900 / 450 / 300 ns** — *estimates pending measurement*; slog floor includes ~200–300 ns of host-handler cost the bridge cannot remove |
| Disabled-level writes | ~3 ns | **no regression** |
| BufferedSink append (v1.1) | n/a | **≤ 100 ns, never blocks** |

Quality gates (all releases): matrix benchmarks at 0/8/32/128 fields,
rates 0/0.05/0.5/1, policies 0/1/16/128, enabled+disabled levels,
serial+parallel; encoder property test vs the zerolog table + 1M-exec
fuzz; **golden gate = parsed field-set equivalence** with the v0.5
zerolog adapter (not byte equality — v0 map order is random) with an
explicit exception list: time value, level casing, and error shape as
produced by each adapter; all modules `-race`; retention matrix
(failure, panic, canceled, timeout) green.

## 5. Review amendments (external panel, 2026-08-30)

Four models (deepseek-v4-pro, qwen3.7-max, glm-5.3 via pi; big-pickle
via opencode) reviewed the locked design. Verdict: 4/4
**ship-with-changes**. Accepted amendments, integrated above and into
the ledger:

1. **Arming protocol specified (was: "fast path never touches the
   mutex" — false as stated).** Every append performs one atomic load
   of the armed flag (~1 ns, budgeted in the 20 ns gate). Unarmed: pure
   append. Armed: append takes the guarded-mode mutex; the watchdog
   snapshots under the same mutex. The transition itself is a
   mutex-protected flag flip — no torn records, `-race` clean.
2. **Sink concurrency contract added.** The watchdog may `Write`
   `operation_stalled` while the request goroutine later `Write`s the
   commit; the drainer writes concurrently by design. Contract: **Sink
   implementations must be safe for concurrent use.** First-party sinks
   satisfy this internally (per-write pooled buffers); the contract is
   one sentence on the box.
3. **Duplicate keys: dedupe moved from Add-time to encode-time.** The
   Add-time scan was O(n) per field (O(n²) per request — fatal at the
   128-field matrix point). Add is now pure append; the encoder
   resolves last-write-wins in one pass with a small seen-set that only
   allocates when duplicates actually exist.
4. **Error bypass is structural, not sampler-dependent.** Errors and
   panics bypass rate sampling *before* any custom `Sampler` runs —
   `KeepErrors` semantics are guaranteed, not user-optional.
5. **Time semantics: v0 parity.** The `time` field is the completion
   time (one `time.Now()` at `End`, budgeted) — the start-reading reuse
   covers `duration_ms` and timeline bases, not the event timestamp,
   which alerting consumers sort on. First-party default format matches
   v0 zerolog-adapter output (RFC3339 string via the cached-prefix
   formatter, ~40 ns); epoch-ms is opt-in. The prior claim that
   epoch-ms is "what zerolog users already ingest" was factually wrong
   — zerolog's default is an RFC3339 string.
6. **`Record.Encoded()` is lazy-once**: populated on first call via
   atomic pointer publish; dropped events never encode; concurrent
   callers race benignly on the publish.
7. **`End` is one-shot and specified**: second call is a no-op
   returning the first result; `bool` = *event emitted* (kept by the
   sampler and written). Pool return is wrapped so a panicking sink
   cannot leak the buffer.
8. **`SampleInput.Fields()` added** — restores v0 `EventFields`
   iteration capability for samplers that scan or dump.
9. **Sample sink examples must copy** out of `Encoded()`; docs fixed.
10. **jsontext fallback demoted** from shipped backend to a
    `benches/`-only comparator behind a build tag — the exit ramp is
    the internal interface, not a second production backend.
11. **BufferedSink spec tightened**: SPSC segment ring — producers
    append to the active segment and swap on full; the drainer owns
    swapped segments and writes outside any producer lock. New gate:
    append latency ≤ 100 ns *measured against a stalled destination*.
12. **Golden and bridge gates restated** as parsed-equivalence and
    estimates-pending-measurement respectively (§4).

Rejected panel suggestions: dropping the `Add` variadic (the ergonomic
win outweighs a documented, per-shape-stated allocation); and removing
last-write-wins semantics (encode-time dedupe preserves them at
measurable cost only when duplicates exist — and duplicates are rare).

## 6. API refinement amendments (owner-approved, 2026-08-30)

Stdlib-alignment pass, benchmarked against `log/slog` conventions:

13. **`MustCompile(cfg) *Runtime`** joins `Compile` — the
    `regexp.Compile`/`MustCompile` idiom for literal configs in `main`.
14. **`Sink.Write(ctx context.Context, rec *Record)`** — the
    `slog.Handler.Handle` shape. The operation's real context reaches
    sinks at commit (trace spans, cancellation); watchdog and drainer
    pass `context.Background()`. Decided now because adding a parameter
    after v1.0 breaks every custom sink.
15. **`Level` is int-backed with `String()`** — like slog, zap, and
    zerolog. Constant call sites stay source-compatible; the wire
    format is unchanged.
16. **The `Add` family returns nothing.** With no event attached they
    are silent no-ops, exactly like `slog.Info`. `End` keeps its
    `bool` (event emitted) — the one return anyone branches on.
17. **Compile error contract**: sentinel errors (`ErrInvalidRate`,
    `ErrInvalidLevel`, `ErrInvalidOutcome`), `%w` wrapping, `"hc: "`
    message prefix. Nil sink stays a valid no-op runtime.
18. **`AddRaw` renamed `AddRawJSON`** (zerolog's community-standard
    name); `Record`/`Field` explicitly documented as following
    `slog.Record`/`slog.Value` conventions.
19. **Runnable examples are a v1.0 gate**: `example_test.go` with
    output-checked `Example` functions for every public constructor
    and the request lifecycle — the strongest std-library signal there
    is. Documented deviations from slog, on purpose: `Add` requires
    the first key (slog's loose variadic produces `"!BADKEY"` garbage;
    our shape makes invalid calls unrepresentable), and no options
    structs exist until an option survives contact with reality.

## 7. evlog audit and OTel decision (owner-approved, 2026-08-30)

A detailed feature audit of `HugoRCD/evlog` (the TypeScript
wide-events library sharing this philosophy) produced one required
fix, one integration decision, and a parking lot.

20. **Sealing — required, not optional.** After `End` commits (or a
    sampler drop recycles the buffer), a straggler `hc.Add` from
    async work that outlived the request would write into a pooled,
    possibly-reused buffer — a use-after-recycle bug class that v0's
    mutex-and-map hid but v2's pooling makes real (evlog calls this
    sealing and warns with dropped keys). Spec: every WAL mutation
    checks a sealed flag (one atomic load, folded into the existing
    armed-flag load); post-`End` writes are no-ops. Build debug mode
    may log dropped keys.
21. **OTel: correlate, don't build — parked post-1.0.** Tracing stays
    out of scope; when integration happens it is a small bridge
    module in the adapter pattern (never core): stamp
    `trace_id`/`span_id` onto the canonical event from the request
    context (OTel API-only dependency, or W3C header parsing in
    integrations for zero deps), and optionally an OTLP-log sink so
    standard backends ingest the events. The black-box timeline
    remains shelved per owner decision; if it ever ships it is framed
    as the no-tracer fallback, not a tracing substitute.

Parking lot (noted, not scheduled): `fork`-style parent-ID
correlation for child operations (one `ParentID` field); a redaction
wrapper sink (`hc.Redact(keys...)`); retry-with-backoff before
drop-oldest in the W11 drainer; an `evlog map`-style static analyzer
scoring handler instrumentation coverage as a CI gate; dev pretty
tree output; `WithPreset` shape option on `NewJSONSink` (three key
names + time format — ~30 lines when asked); **`adapter/otlp`** — the
one destination adapter worth shipping (post-1.0, rides W11's batching
machinery; OTLP covers Datadog/Honeycomb/Grafana/Better Stack via
their endpoints) plus a documented 20-line custom-destination sink
recipe. Rejected as out of scope: audit signing/hash-chains, AI
token accounting, why/fix/docs-link error affordances (TS DX
idioms with no Go equivalent worth inventing), and **per-vendor
destination drains** (`adapter/datadog`, `adapter/posthog`, …) — in
Go the collector/agent owns that job, and stdout-plus-agent is the
12-factor norm evlog's Node-only world lacks.

## 8. Decisions ledger

Every question raised during design, and its final answer.

| Question | Decision |
| --- | --- |
| Version number of the break | **v1.0.0**; "v2" is the architecture codename (v0.5.0 precedent set) |
| Event storage | typed `[]Field` WAL records — not a map, not a pre-encoded buffer |
| Field order | insertion order; structural; no sort anywhere (removed in v0.5.0) |
| Duplicate keys | pure append at Add; last-write-wins resolved at encode with an on-demand seen-set (amendment 3) |
| Concurrency | request-confined; **one atomic armed-flag load per append**; guarded-mode mutex only when armed; Sinks must be concurrency-safe |
| Sink contract | `Write(ctx, *Record)` read-only view (slog.Handle shape); no clone; no retention; concurrency-safe |
| Field introspection | `Lookup` + `Fields()` on SampleInput and Record; no live-Event access; `FromContext` removed |
| Time field | completion-time RFC3339 string (v0 parity) by default; epoch-ms opt-in; start-reading reused for duration/timeline |
| Encoder | forked zerolog `internal/json` + hybrid SWAR; jsontext kept as a benches-only comparator behind a build tag |
| Third-party JSON libs | none (sonic/goccy rejected with measurements, §3b of plan) |
| Config | `Compile`/`MustCompile` once; `NormalizeConfig` retired; integrations take `*Runtime`; nil sink = no-op runtime |
| Lifecycle | `Start` + `End(&err)` only; begin/finish path internal |
| Outcome precedence | panic > error > explicit > 5xx > success |
| Canonical fields | `op.*`; `http.method/path/route/status` for HTTP; `op.code` non-HTTP only; worker `job.*` mirrors dropped |
| Bridges | `New` only; `SinkOptions` removed entirely at 1.0 |
| Sinks per config | single; compose (`BufferedSink`, custom fan-out with `Encoded()` reuse) |
| Buffered sink | opt-in; drop-oldest after optional retry-backoff; counted; `Flush` at shutdown |
| WAL lifecycle | sealed after `End` — straggler writes are no-ops (amendment 20) |
| Tracing / OTel | out of scope to build; correlate via bridge module post-1.0 (amendment 21); timeline shelved |
| Destination adapters | bridges (slog/zap/zerolog) = formats, shipped at 1.0; `adapter/otlp` = the only first-party destination, post-1.0; per-vendor drains rejected — the Sink interface plus a documented recipe covers custom destinations |
| Timeline / watchdog | off by default; arm-on-stall; `t_ms` only when armed |
| Disk-backed WAL | rejected — shipper territory |
| Compatibility window | none needed — zero users; `v2` is the only release line (owner, 2026-08-31), main frozen at v0.5.0; port-back lane retired |

## 9. Branching and release choreography

> **Amended 2026-08-31 (owner decision): the dual-line choreography is
> retired.** With zero external users, the 0.x compatibility window it
> protected doesn't exist. `v2` is the single development and release
> line; `main` is frozen at v0.5.0 behind a pointer banner. Version
> mechanics are unchanged — `feat:` on v2 releases 0.6.0, and the
> record-core `feat!:` computes 1.0.0 through the same release-please
> flow, just aimed at v2. The port-back lane is dead: nothing flows to
> main anymore.

1. **v0.6.0 ships from v2** — W1 encoder fork + W2 `NewJSONSink` plus
   the Go-floor/CI PR merge into v2; release-please opens the release
   PR there (the release workflow triggers on v2 pushes; CI gates v2
   PRs).
2. **The break develops on v2** — sequenced PRs targeting the `v2`
   branch: core first (W3 typed WAL with sealing and arming), then W5
   Compile/Runtime, W4 Record/Sink, W7 lifecycle, W6 sampler, then W8
   bridges, W9 dedupe, integrations, `MIGRATION.md`, runnable examples.
   Every PR carries the full matrix, `-race`, and benchstat evidence
   against the §4 gates.
3. **v1.0.0 releases from v2** — when the S-series merges, release-
   please computes 0.6.x → 1.0.0 from the breaking marker and lockstep
   scripts tag all nested modules 1.0.0. No cutover merge, no feature
   freeze, no classic-line retirement PR.
4. **main** — frozen at v0.5.0 with a README banner pointing at the v2
   line; the 0.x tags keep a home. Afterwards, either switch the
   default branch to v2 or fast-forward main to v2 (a plain merge —
   main never releases again).

## 10. What would reopen this design

Only three things: a gate that cannot be met after two honest
optimization attempts (then the gate, not the design, is renegotiated);
a correctness bug in the WAL/encoder model that property tests can't
patch; or Go itself changing the cost structure (e.g. a stdlib append
encoder that beats the fork — then the internal interface swaps, which
is exactly why it exists).
