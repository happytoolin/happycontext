# unolog

> **This branch is a snapshot at v1.0.0.** Development and future
> releases continue on [`v2`](https://github.com/happytoolin/unolog/tree/v2).

![unolog banner](./assets/og-image.svg)

[![CI](https://github.com/happytoolin/unolog/actions/workflows/ci.yml/badge.svg)](https://github.com/happytoolin/unolog/actions/workflows/ci.yml)
[![Release](https://github.com/happytoolin/unolog/actions/workflows/release.yml/badge.svg)](https://github.com/happytoolin/unolog/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/happytoolin/unolog.svg)](https://pkg.go.dev/github.com/happytoolin/unolog)
[![Go Version](https://img.shields.io/badge/go-1.25%2B-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

Most application logs are high-volume but low-context.
`unolog` helps Go services emit one structured, canonical event per request, so debugging and analysis start from a complete record instead of scattered lines.

> Formerly **happycontext**. Pre-1.0 releases remain available under the old
> `github.com/happytoolin/happycontext` module path; this repository is their
> renamed continuation. See [`MIGRATION.md`](./MIGRATION.md).

![unolog log stream demo](./assets/demo-log-stream.svg)

## Why unolog?

- Cleaner logs with one canonical event per request
- Consistent fields across handlers, middleware, and frameworks
- Built-in sampling for healthy traffic
- Error and panic events are always preserved
- Works with `slog`, `zap`, and `zerolog`
- Integrates with `net/http`, `gin`, `echo`, `fiber`, `fiber v3`, and worker jobs

Design principle:

- Prefer one context-rich request event over many fragmented log lines.
  ![unolog before and after](./assets/demo-before-after.svg)

## Install

```bash
go get github.com/happytoolin/unolog
go get github.com/happytoolin/unolog/adapter/slog
go get github.com/happytoolin/unolog/integration/std
```

Install only the adapter and integration packages you use.

## Quick Start (`net/http` + `slog`)

Compile the runtime once, wrap the handler, annotate with `unolog.Add`:

```go
package main

import (
	"errors"
	"log/slog"
	"net/http"
	"os"

	"github.com/happytoolin/unolog"
	uslog "github.com/happytoolin/unolog/adapter/slog"
	"github.com/happytoolin/unolog/integration/std"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := uslog.New(logger)

	rt := unolog.MustCompile(unolog.Config{
		Sink:         sink,
		SamplingRate: 1.0,
	})
	mw := std.Middleware(rt)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		unolog.Add(r.Context(), "user_id", "u_8472", "feature", "checkout")
		if r.URL.Query().Get("fail") == "1" {
			unolog.SetMessage(r.Context(), "checkout_failed")
			unolog.Error(r.Context(), errors.New("checkout failed"))
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		unolog.SetMessage(r.Context(), "checkout_succeeded")
		w.WriteHeader(http.StatusOK)
	})

	_ = http.ListenAndServe(":8080", mw(mux))
}
```

Other quick starts:

- `net/http + zap` and `net/http + zerolog` are in `## More Examples`
- `gin`, `echo`, `fiber v2`, and `fiber v3` (with `slog`) are in `## More Examples`
- Runnable reference apps are in `cmd/examples`
- Zero-dependency output: `unolog.NewJSONSink(os.Stdout)` needs no logger at all

## Quick Start (Background Job)

```go
func runImport(ctx context.Context, rt *unolog.Runtime) (err error) {
	op := unolog.Start(ctx, rt, unolog.OperationStart{
		Domain:      unolog.DomainJob,
		Name:        "import",
		ID:          "job_8472",
		Attempt:     2,
		MaxAttempts: 3,
	})
	defer op.End(&err) // captures errors AND panics; re-panics

	unolog.Add(op.Context(), "rows", 42, "source", "queue")
	return doImport(ctx)
}
```

One event, always: header fields, handler fields in attach order, and
completion fields — deterministic by construction. Failures are never
sampled away.

## Configuration

Compile once at startup; `*unolog.Runtime` is immutable and shared by all
requests. Invalid configuration is a construction-time error
(`unolog.ErrInvalidRate`, `unolog.ErrInvalidLevel`, `unolog.ErrInvalidOutcome`) —
use `unolog.Compile` for config from files, `unolog.MustCompile` for literals.

`unolog.Config` gives you the core controls:

- `Sink`: destination logger adapter (required to emit events)
- `SamplingRate`: `0` drops healthy events, `1` keeps all healthy events
- `LevelSamplingRates`: optional level-specific sampling overrides
- `Sampler`: optional custom sampling function (full control)
- `OperationPolicies`: optional per-domain level/sampling policy for all lifecycle domains, including HTTP and background operations; domain sampling overrides generic level/default sampling
- Precedence when both are set: a domain policy's `SamplingRate` overrides `LevelSamplingRates` for that domain (v0 behavior); `LevelSamplingRates` overrides the global `SamplingRate`
- `Message`: final log message (defaults to `unolog.DefaultMessage` for HTTP and `unolog.DefaultOperationMessage` for non-HTTP)

Notes:

- Sampling is automatically bypassed for errors and server failures.
- If no sink is configured, requests still run; logging is skipped.
- Sampling behavior is consistent across all integrations (`net/http`, `gin`, `echo`, `fiber`, and `fiber v3`).
- `unolog.SetMessage(ctx, "...")` overrides `Config.Message` for a single event.

### Per-request Message Override

Use `unolog.SetMessage` when a route or handler should emit a more specific final message than the integration-wide default:

```go
func checkoutHandler(w http.ResponseWriter, r *http.Request) {
	if err := processCheckout(r.Context()); err != nil {
		unolog.SetMessage(r.Context(), "checkout_failed")
		unolog.Error(r.Context(), err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	unolog.SetMessage(r.Context(), "checkout_succeeded")
	w.WriteHeader(http.StatusOK)
}
```

Passing an empty string leaves the event on the configured default message.

Errors are recorded as structured metadata:

```json
{
  "error": {
    "message": "checkout failed",
    "type": "*errors.errorString"
  }
}
```

### Sampling Customization

Per-level sampling:

```go
mw := std.Middleware(unolog.MustCompile(unolog.Config{
	Sink:         sink,
	SamplingRate: 0.05, // default for healthy traffic
	LevelSamplingRates: map[unolog.Level]float64{
		unolog.LevelWarn:  1.0, // keep all warns
		unolog.LevelDebug: 0.01,
	},
}))
```

Custom sampler (route/user/latency rules):

```go
mw := std.Middleware(unolog.MustCompile(unolog.Config{
	Sink: sink,
	Sampler: func(in unolog.SampleInput) bool {
		// Always keep failures and slow requests.
		if in.HasError || in.StatusCode >= 500 {
			return true
		}
		if in.Duration > 2*time.Second {
			return true
		}
		// Keep checkout requests.
		if in.Path == "/api/checkout" {
			return true
		}
		// Keep enterprise requests based on event fields.
		tier, _ := in.Lookup("user_tier")
		return tier == "enterprise"
	},
}))
```

`unolog.Add` accepts one or more key/value pairs:
`unolog.Add(ctx, "k1", v1, "k2", v2, "k3", v3)`.

`unolog.SampleInput.Method`, `Path`, and `StatusCode` are HTTP compatibility fields.
For non-HTTP operations use `Domain`, `Operation`, `Outcome`, and `Code`.

Built-in sampler chain:

```go
mw := std.Middleware(unolog.MustCompile(unolog.Config{
	Sink: sink,
	Sampler: unolog.ChainSampler(
		unolog.RateSampler(0.05),        // base sampler
		unolog.KeepErrors(),             // always keep errors
		unolog.KeepPathPrefix("/admin"), // always keep admin paths
		unolog.KeepSlowerThan(500*time.Millisecond),
	),
}))
```

Sampler building blocks:

- `unolog.ChainSampler(base, middlewares...)`: composes one final `Sampler` from middleware rules.
- `unolog.AlwaysSampler()`: base sampler that keeps every event.
- `unolog.NeverSampler()`: base sampler that drops every event.
- `unolog.RateSampler(rate)`: base probabilistic sampler (`0` drops all, `1` keeps all).
- `unolog.KeepErrors()`: middleware that keeps errored requests (`HasError` or `5xx`).
- `unolog.KeepPathPrefix("/checkout", "/admin")`: middleware that keeps matching path prefixes.
- `unolog.KeepSlowerThan(minDuration)`: middleware that keeps requests at/above a duration threshold.

### Generic Operation Lifecycle API

For non-HTTP flows, use `unolog.Start` with the compiled runtime:

```go
func runJob(ctx context.Context, rt *unolog.Runtime) (err error) {
	op := unolog.Start(ctx, rt, unolog.OperationStart{
		Domain: unolog.DomainJob,
		Name:   "invoice.reconcile",
		ID:     "job_1001",
		Source: "nightly",
	})
	defer op.End(&err) // direct defer: captures errors and panics

	unolog.Add(op.Context(), "account_id", "acct_42")
	return nil
}
```

`op.End(&err)` is the only completion path: one-shot, returning whether
the event was emitted. The `worker` integration wraps this idiom for
queue consumers.

## Integrations

- `integration/std` (`net/http`)
- `integration/gin`
- `integration/echo`
- `integration/fiber` (Fiber v2)
- `integration/fiberv3` (Fiber v3)
- `integration/worker` (background jobs/non-HTTP operations)

## Logger Adapters

- `adapter/slog`
- `adapter/zap`
- `adapter/zerolog`

Adapters expose `New` only (the `SinkOptions`/`NewWithOptions` shapes
were removed at 1.0: nothing to configure until proven otherwise).
Fields arrive in insertion order, deterministically, as typed
constructors.

### First-party JSON sink (no logger dependency)

`unolog.NewJSONSink(w io.Writer)` emits the same canonical event shape as the
zerolog adapter — lowercase `level`, RFC3339 `time`, your fields, `message`
last — as one JSON line per event, with zero dependencies beyond the
standard library (the module's only non-stdlib require is
go.uber.org/goleak — a test-only dependency used by the package's
goroutine-leak check, never linked into consumers):

```go
sink := unolog.NewJSONSink(os.Stdout)
```

The wire format matches `zerolog.New(w).With().Timestamp().Logger()`
through `adapter/zerolog`, so existing pipelines ingest it unchanged.
Field order is insertion order — deterministic by construction, identical across every sink.

## More Examples

<details>
<summary>1. net/http + slog</summary>

```go
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/happytoolin/unolog"
	uslog "github.com/happytoolin/unolog/adapter/slog"
	"github.com/happytoolin/unolog/integration/std"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := uslog.New(logger)
	mw := std.Middleware(unolog.MustCompile(unolog.Config{Sink: sink, SamplingRate: 1}))

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		unolog.Add(r.Context(), "router", "net/http")
		w.WriteHeader(http.StatusOK)
	})

	_ = http.ListenAndServe(":8101", mw(mux))
}
```

</details>

<details>
<summary>2. gin + slog</summary>

```go
package main

import (
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/happytoolin/unolog"
	uslog "github.com/happytoolin/unolog/adapter/slog"
	ugin "github.com/happytoolin/unolog/integration/gin"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := uslog.New(logger)

	r := gin.New()
	r.Use(ugin.Middleware(unolog.MustCompile(unolog.Config{Sink: sink, SamplingRate: 1})))
	r.GET("/users/:id", func(c *gin.Context) {
		unolog.Add(c.Request.Context(), "router", "gin")
		c.Status(200)
	})

	_ = r.Run(":8105")
}
```

</details>

<details>
<summary>3. fiber v2 + slog</summary>

```go
package main

import (
	"log/slog"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/happytoolin/unolog"
	uslog "github.com/happytoolin/unolog/adapter/slog"
	ufiber "github.com/happytoolin/unolog/integration/fiber"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := uslog.New(logger)

	app := fiber.New()
	app.Use(ufiber.Middleware(unolog.MustCompile(unolog.Config{Sink: sink, SamplingRate: 1})))
	app.Get("/users/:id", func(c *fiber.Ctx) error {
		unolog.Add(c.UserContext(), "router", "fiber-v2")
		return c.SendStatus(200)
	})

	_ = app.Listen(":8107")
}
```

</details>

<details>
<summary>4. fiber v3 + slog</summary>

```go
package main

import (
	"log/slog"
	"os"

	"github.com/gofiber/fiber/v3"
	"github.com/happytoolin/unolog"
	uslog "github.com/happytoolin/unolog/adapter/slog"
	ufiberv3 "github.com/happytoolin/unolog/integration/fiberv3"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := uslog.New(logger)

	app := fiber.New()
	app.Use(ufiberv3.Middleware(unolog.MustCompile(unolog.Config{Sink: sink, SamplingRate: 1})))
	app.Get("/users/:id", func(c fiber.Ctx) error {
		unolog.Add(c.Context(), "router", "fiber-v3")
		return c.SendStatus(200)
	})

	_ = app.Listen(":8108")
}
```

</details>

<details>
<summary>5. echo + slog</summary>

```go
package main

import (
	"log/slog"
	"os"

	"github.com/happytoolin/unolog"
	uslog "github.com/happytoolin/unolog/adapter/slog"
	uecho "github.com/happytoolin/unolog/integration/echo"
	"github.com/labstack/echo/v4"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := uslog.New(logger)

	e := echo.New()
	e.Use(uecho.Middleware(unolog.MustCompile(unolog.Config{Sink: sink, SamplingRate: 1})))
	e.GET("/users/:id", func(c echo.Context) error {
		unolog.Add(c.Request().Context(), "router", "echo")
		return c.NoContent(200)
	})

	_ = e.Start(":8106")
}
```

</details>

<details>
<summary>6. net/http + zap</summary>

```go
package main

import (
	"net/http"

	"github.com/happytoolin/unolog"
	uzap "github.com/happytoolin/unolog/adapter/zap"
	"github.com/happytoolin/unolog/integration/std"
	"go.uber.org/zap"
)

func main() {
	logger := zap.NewExample()
	sink := uzap.New(logger)
	mw := std.Middleware(unolog.MustCompile(unolog.Config{Sink: sink, SamplingRate: 1}))

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		unolog.Add(r.Context(), "example", "adapter-zap")
		w.WriteHeader(http.StatusOK)
	})

	_ = http.ListenAndServe(":8102", mw(mux))
}
```

</details>

<details>
<summary>7. net/http + zerolog</summary>

```go
package main

import (
	"net/http"
	"os"

	"github.com/happytoolin/unolog"
	uzerolog "github.com/happytoolin/unolog/adapter/zerolog"
	"github.com/happytoolin/unolog/integration/std"
	"github.com/rs/zerolog"
)

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	sink := uzerolog.New(&logger)
	mw := std.Middleware(unolog.MustCompile(unolog.Config{Sink: sink, SamplingRate: 1}))

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		unolog.Add(r.Context(), "example", "adapter-zerolog")
		w.WriteHeader(http.StatusOK)
	})

	_ = http.ListenAndServe(":8103", mw(mux))
}
```

</details>

Runnable commands are also available in `cmd/examples`:

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
go run ./sampling-inbuilt
go run ./sampling-custom
go run ./worker-job
```

## Release Process

- CI: `.github/workflows/ci.yml`
- Release automation: `.github/workflows/release.yml`
- Go proxy sync: `.github/workflows/go-proxy-sync.yml`
- Root module releases must be tagged as `vX.Y.Z`.
- Nested Go modules must be tagged as `<subdir>/vX.Y.Z` so `go list -m -versions` can discover them.
- To backfill historical tags created with the old `unolog-vX.Y.Z` format, run `./scripts/backfill-go-tags.sh` and push the generated tags.

Published nested modules:

- `adapter/slog`
- `adapter/zap`
- `adapter/zerolog`
- `integration/echo`
- `integration/fiber`
- `integration/fiberv3`
- `integration/gin`
- `integration/std`
- `integration/worker`

## Migrating from v0.x

Coming from a 0.x release? [`MIGRATION.md`](./MIGRATION.md) maps every
removed or changed symbol, the outcome-precedence change, and the wire
format differences.

## References

- Framing inspiration: "Logging Sucks - Your Logs Are Lying To You" by Boris Tane: https://loggingsucks.com/

## License

MIT
