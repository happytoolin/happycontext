# happycontext

`happycontext` is a request-scoped structured logging library for Go.
It implements the Canonical Log Line (wide event) pattern: collect fields during request handling, then emit one final event when the request completes.

## Why happycontext

- One event per request instead of many fragmented log lines
- Consistent field contract across handlers, middleware, and services
- Built-in request-level sampling with error bypass
- Framework-specific middleware integrations
- Adapter modules for `slog`, `zap`, and `zerolog`

## Install

Compatibility:

- Core and standard integrations: Go `1.24+`
- Fiber v3 integration (`integration/fiberv3`): Go `1.25+`
- Examples module (`cmd/examples`): Go `1.25+`


Core package:

```bash
go get github.com/happytoolin/happycontext
```

Choose only the modules you use:

```bash
go get github.com/happytoolin/happycontext/adapter/slog
go get github.com/happytoolin/happycontext/adapter/zap
go get github.com/happytoolin/happycontext/adapter/zerolog
go get github.com/happytoolin/happycontext/integration/std
go get github.com/happytoolin/happycontext/integration/gin
go get github.com/happytoolin/happycontext/integration/echo
go get github.com/happytoolin/happycontext/integration/fiber
go get github.com/happytoolin/happycontext/integration/fiberv3
```

## Quick Start (net/http + slog)

```go
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/happytoolin/happycontext"
	slogadapter "github.com/happytoolin/happycontext/adapter/slog"
	stdhc "github.com/happytoolin/happycontext/integration/std"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := slogadapter.New(logger)

	mw := stdhc.Middleware(hc.Config{
		Sink:         sink,
		SamplingRate: 0.10,
		Message:      "request_completed",
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		hc.Add(ctx, "user_id", "u_8472")
		hc.Add(ctx, "feature", "checkout")
		w.WriteHeader(http.StatusOK)
	})

	_ = http.ListenAndServe(":8080", mw(mux))
}
```

## Core API

- `hc.NewContext(ctx)` creates a context with an attached event
- `hc.Add(ctx, key, value) bool` adds or overwrites one field
- `hc.AddMap(ctx, map[string]any) bool` merges fields
- `hc.Error(ctx, err) bool` stores structured error metadata and marks the event as failed
- `hc.SetLevel(ctx, level) bool` requests a minimum final level (`hc.LevelDebug`, `hc.LevelInfo`, `hc.LevelWarn`, `hc.LevelError`)
- `hc.SetRoute(ctx, route) bool` sets low-cardinality route template (`/orders/:id`)
- `hc.Commit(ctx, sink, level) bool` immediately writes one snapshot (manual lifecycle)

## Event Fields

Middleware integrations emit a consistent base contract:

- `http.method` request method
- `http.path` raw request path
- `http.route` route template when available
- `http.status` final committed status code
- `duration_ms` request duration in milliseconds
- `error` structured error object (`message`, `type`) when present
- `panic` structured panic object (`type`, `value`) when recovered

## Level Resolution

Final level is computed as:

1. Auto level: `INFO` by default, `ERROR` when request failed
2. Optional requested level from `SetLevel`
3. Floor merge: requested level may raise severity, never lower auto level

Examples:

- auto `INFO` + requested `WARN` => final `WARN`
- auto `ERROR` + requested `DEBUG` => final `ERROR`

## Sampling Rules

Sampling happens at request finalization:

- Requests with errors (`hc.Error`) are always logged
- Requests with status `>= 500` are always logged
- Healthy requests follow `SamplingRate`:
  - `0` never log
  - `1` always log
  - `0 < rate < 1` probabilistic log

## Integrations

- `integration/std` for `net/http`
- `integration/gin` for Gin
- `integration/echo` for Echo
- `integration/fiber` for Fiber v2
- `integration/fiberv3` for Fiber v3

Each integration is a separate Go module to keep dependency footprints small.

## Adapters

- `adapter/slog`
- `adapter/zap`
- `adapter/zerolog`

All adapters implement `hc.Sink`.

## Manual Lifecycle Example

```go
ctx, _ := hc.NewContext(context.Background())
hc.Add(ctx, "job.id", "j_123")
hc.Add(ctx, "duration_ms", 42)
_ = hc.Commit(ctx, sink, hc.LevelInfo)
```

## Testing

Use the in-memory sink in tests:

```go
sink := hc.NewTestSink()
// ... trigger request ...
events := sink.Events()
```

## Examples

Runnable examples are in `cmd/examples`:

```bash
cd cmd/examples
go run ./adapter-slog
go run ./adapter-zap
go run ./adapter-zerolog
go run ./router-std
go run ./router-gin
go run ./router-echo
go run ./router-fiber
go run ./router-fiberv3
```

## Benchmarks

```bash
just bench
just bench-core
just bench-nonhttp
just bench-adapters
just bench-integrations
just bench-fiberv3-middleware
just bench-middleware-overhead
just bench-normal-logging
just bench-router-comparison
just bench-save-all baseline
just bench-compare-all baseline current
```

## License

MIT
