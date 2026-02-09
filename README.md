# happycontext

`happycontext` helps Go services emit one structured log event per request.
Instead of scattered log lines across handlers and middleware, you collect context during execution and write a single, complete event at the end.

## Why use it?

- Cleaner logs with one canonical event per request
- Consistent fields across handlers, middleware, and frameworks
- Built-in sampling for healthy traffic
- Error and panic events are always preserved
- Works with `slog`, `zap`, and `zerolog`.
- Integrates with `net/http`, `gin`, `echo`, `fiber`, and `fiber v3`.

## Install

```bash
go get github.com/happytoolin/happycontext
go get github.com/happytoolin/happycontext/adapter/slog
go get github.com/happytoolin/happycontext/integration/std
```

Use only the adapter and integration modules you need.

## Quick Start (`net/http` + `slog`)

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
		SamplingRate: 1.0,
		Message:      "request_completed",
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		hc.Add(r.Context(), "user_id", "u_8472")
		hc.Add(r.Context(), "feature", "checkout")
		w.WriteHeader(http.StatusOK)
	})

	_ = http.ListenAndServe(":8080", mw(mux))
}
```

## Configuration

`hc.Config` gives you the core controls:

- `Sink`: destination logger adapter (required to emit events)
- `SamplingRate`: `0` to `1` for healthy-request sampling
- `Message`: final log message (defaults to `request_completed`)

Notes:

- Sampling is automatically bypassed for errors and server failures.
- If no sink is configured, requests still run; logging is skipped.

## Integrations

- `integration/std` (`net/http`)
- `integration/gin`
- `integration/echo`
- `integration/fiber` (Fiber v2)
- `integration/fiberv3` (Fiber v3)

## Logger Adapters

- `adapter/slog`
- `adapter/zap`
- `adapter/zerolog`

## More Examples

Runnable examples are available in `cmd/examples`:

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

## Release Process

- CI: `.github/workflows/ci.yml`
- Release automation: `.github/workflows/release.yml`
- Go proxy sync: `.github/workflows/go-proxy-sync.yml`

## License

MIT
