# hlog

`hlog` is a request-scoped structured logging library for Go.
It implements the Canonical Log Line (wide event) pattern: collect fields during request handling, then emit one final event when the request completes.

## Why hlog

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
go get github.com/happytoolin/hlog
```

Choose only the modules you use:

```bash
go get github.com/happytoolin/hlog/adapter/slog
go get github.com/happytoolin/hlog/adapter/zap
go get github.com/happytoolin/hlog/adapter/zerolog
go get github.com/happytoolin/hlog/integration/std
go get github.com/happytoolin/hlog/integration/gin
go get github.com/happytoolin/hlog/integration/echo
go get github.com/happytoolin/hlog/integration/fiber
go get github.com/happytoolin/hlog/integration/fiberv3
```

## Quick Start (net/http + slog)

```go
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/happytoolin/hlog"
	slogadapter "github.com/happytoolin/hlog/adapter/slog"
	stdhlog "github.com/happytoolin/hlog/integration/std"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := slogadapter.New(logger)

	mw := stdhlog.Middleware(hlog.Config{
		Sink:         sink,
		SamplingRate: 0.10,
		Message:      "request_completed",
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		hlog.Add(ctx, "user_id", "u_8472")
		hlog.Add(ctx, "feature", "checkout")
		w.WriteHeader(http.StatusOK)
	})

	_ = http.ListenAndServe(":8080", mw(mux))
}
```

## Core API

- `hlog.NewContext(ctx)` creates a context with an attached event
- `hlog.Add(ctx, key, value)` adds or overwrites one field
- `hlog.AddMap(ctx, map[string]any)` merges fields
- `hlog.Error(ctx, err)` stores structured error metadata and marks the event as failed
- `hlog.SetLevel(ctx, level)` requests a minimum final level (`DEBUG`, `INFO`, `WARN`, `ERROR`)
- `hlog.SetRoute(ctx, route)` sets low-cardinality route template (`/orders/:id`)
- `hlog.Commit(ctx, sink, level)` immediately writes one snapshot (manual lifecycle)

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

- Requests with errors (`hlog.Error`) are always logged
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

All adapters implement `hlog.Sink`.

## Manual Lifecycle Example

```go
ctx, _ := hlog.NewContext(context.Background())
hlog.Add(ctx, "job.id", "j_123")
hlog.Add(ctx, "duration_ms", 42)
hlog.Commit(ctx, sink, hlog.LevelInfo)
```

## Testing

Use the in-memory sink in tests:

```go
sink := hlog.NewTestSink()
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
```

## License

MIT
