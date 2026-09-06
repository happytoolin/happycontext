# Migrating from v0.x to v1.0.0

v1.0.0 is the v2 record core: the same one-canonical-event-per-request
philosophy on a typed, insertion-ordered WAL instead of a map. This
guide maps every removed or changed symbol. The
[wire format](#wire-format-changes) section covers output differences.

## The mental model

- **v0**: configure per call sites (`hc.Config` handed to middleware or
  `op.End`), fields in a `map[string]any` (random order), sinks receive
  `(level, message, map)`.
- **v1**: `hc.Compile`/`hc.MustCompile` once at startup → an immutable
  `*Runtime` shared by everything; fields are typed `hc.Field` records
  in insertion order; sinks receive `(ctx, *hc.Record)`.

## Construction

| v0 | v1 |
|---|---|
| `hc.Config{...}` passed to middleware / `op.End(cfg, &err)` | `rt := hc.MustCompile(hc.Config{...})` once; middleware takes `rt`; `op.End(&err)` |
| `hc.NormalizeConfig(cfg)` | internal to `Compile`; invalid config is now a **construction-time error** (`ErrInvalidRate`, `ErrInvalidLevel`, `ErrInvalidOutcome`, wrapped with `%w`) |
| — | `hc.Compile(cfg) (*Runtime, error)` for config from files/flags |

```go
// v0
mw := stdhc.Middleware(hc.Config{Sink: sink, SamplingRate: 1})

// v1
rt := hc.MustCompile(hc.Config{Sink: sink, SamplingRate: 1})
mw := stdhc.Middleware(rt)
```

## Lifecycle

| v0 | v1 |
|---|---|
| `hc.BeginOperation(ctx, start)` → `(ctx, *Event)` | `op := hc.Start(ctx, rt, start)`; pass `op.Context()` down |
| `hc.StartOperation(ctx, start)` → `*Operation` | `hc.Start(ctx, rt, start)` |
| `op.End(cfg, &err)` | `op.End(&err)` — one-shot, returns *emitted* |
| `hc.FinishOperation(cfg, OperationFinish{...})` | gone; integrations call `op.End` |
| `hc.Commit(ctx, sink, level)` | gone; the event commits once at `op.End` |
| `op.Event()` | gone; the WAL is request-confined (use the sampler view or sinks) |

`End` must be **deferred directly** — `defer op.End(&err)`. The closure
form (`defer func() { op.End(&err) }()`) compiles but silently disables
panic capture.

## Fields

| v0 | v1 |
|---|---|
| `hc.Add(ctx, k, v)` returning `bool` | same call, returns nothing (silent no-op without an event — the `slog` helper idiom) |
| `hc.EventFields(e)` / `EventMessage` / `EventHasMessage` / `EventHasError` / `EventStartTime` | gone; sinks receive `*hc.Record` (`Fields()`, `Lookup`, `Message()`, `Level()`); samplers receive `SampleInput.Fields()`/`Lookup`. HTTP `SampleInput.Operation` is the last-write `op.name` (the route template), not the Start name `"request"`. Non-HTTP `SampleInput.Code` surfaces the canonical `op.code` (`StatusCode` stays the `http.status` view); HTTP `Code` remains `http.status`. |
| `hc.NewContext(ctx)` / `hc.FromContext(ctx)` | gone (internal) |
| `hc.AddRaw(ctx, k, raw)` | gone; pass `json.RawMessage` to `hc.Add` — it embeds verbatim through the regular path with bridge parity |
| — | `hc.SetLevel(ctx, level)` unchanged in shape; `GetLevel` removed |

## Levels

`hc.Level` is now int-backed with slog-compatible ranks
(`LevelDebug = -4`, `LevelInfo = 0`, `LevelWarn = 4`, `LevelError = 8`).
Source that references the constants compiles unchanged;
`hc.LevelRank` and `hc.MergeLevelWithFloor` are gone (internal).

## Sinks

```go
// v0
type Sink interface {
    Write(level hc.Level, message string, fields map[string]any)
}

// v1 — slog.Handler.Handle shape
type Sink interface {
    Write(ctx context.Context, rec *hc.Record)
}
```

- `rec.Fields()` is the insertion-ordered typed slice; `rec.Lookup(key)`
  resolves last-write-wins; `rec.Encoded()` is the canonical JSON line,
  computed once (copy it if you retain it).
- The Record is valid only inside `Write`; sinks must be safe for
  concurrent use.

## Bridges

`NewWithOptions` and `SinkOptions` are removed from all three adapters
(`adapter/slog`, `adapter/zap`, `adapter/zerolog`) — constructors are
`New` only. Bridges iterate `rec.Fields()` in order with typed
constructors.

## Outcome precedence

v0 resolved `explicit outcome > panic > error` in some paths; v1 is
strictly **panic > error > explicit > 5xx > success**. Errors and
panics also **bypass sampling structurally** — before any custom
`Sampler` runs — so `NeverSampler()` can no longer hide failures.

## Wire format changes

- **Field order**: insertion order (deterministic), replacing Go's
  random map order.
- **`op.code`**: emitted only for non-HTTP operations (from the explicit
  `op.code` field); HTTP events carry `http.status`.
- **Worker `job.*` mirrors dropped**: `job.name`, `job.id`,
  `job.queue`, `job.attempt`, `job.max_attempts` are gone — `op.name`,
  `op.id`, `op.source`, `op.attempt`, `op.max_attempts` carry them.
  `job.scheduled_at` remains.
- **Duplicate keys**: last write wins (v0 maps did the same); encoding
  emits each key once.
- **Canonical-key collisions** (the logrus precedent): user fields
  named `message`, `time`, or `level` collide with the canonical
  envelope members, which used to produce duplicate JSON keys on the
  line. They are now renamed to `fields.message`, `fields.time`,
  `fields.level` on the wire (dedupe runs over the renamed keys, so a
  user `message` and a user `fields.message` fold into one last-write-
  wins member). The rename is wire-only: `Record.Fields()`/`Lookup`
  keep the original keys; adapters that consume `Fields()` still see
  them unrenamed.
- **Durations** through bridges render each host's native shape
  (unchanged from v0 adapters); the first-party JSON sink renders float
  milliseconds.
- **float32 values**: the JSON sink and the zap/zerolog bridges render
  32-bit precision (`0.1`); the slog host widens on Go ≥ 1.24
  (`slog.AnyValue` converts to `Float64Value`) — same as the v0
  adapter.
- **Failure levels on default policies**: unchanged (INFO success,
  ERROR failures/panics).

## Removed symbols (complete list)

`BeginOperation`, `FinishOperation`, `OperationFinish`, `StartOperation`,
`Commit`, `GetLevel`, `LevelRank`, `MergeLevelWithFloor`, `EventFields`,
`EventMessage`, `EventHasMessage`, `EventHasError`, `EventStartTime`,
`NewContext`, `FromContext`, `NormalizeConfig`, `AddRaw`/`AddRawJSON` (kind `KindRaw` and `Field.Raw()` went with them), plus the
`Event` type itself, `SinkOptions`/`NewWithOptions` in the bridges, and
`operation/common.NormalizeConfig`-style helpers that moved into
`Compile`.

## Nothing to configure?

A `nil` `*Runtime` is a valid no-op: requests run, nothing emits — the
v0 zero-config behavior, kept deliberately.
