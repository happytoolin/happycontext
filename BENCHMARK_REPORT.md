# happycontext Benchmark & Profiling Report

Date: February 10, 2026
Machine: Apple M4 (darwin/arm64)

## Focused Update: June 6-7, 2026

This pass keeps three useful changes:

- Lower-allocation event storage and typed field/sink paths for small events.
- Faster HTTP integrations through pooled request state, prepared request config, monotonic timing, and guarded unsafe request-context swaps.
- Hot-path operation APIs for callers that can trade ergonomics for fewer allocations.

Focused verification commands:

```bash
(cd bench && go test -run '^$' -bench 'Benchmark(EventAddMany|EventAddFieldsMany|OperationLifecycle)' -benchmem -count=5 ./...)
(cd bench && go test ./integration -run '^$' -bench 'BenchmarkRouter_(std|gin|echo)/middleware_on_sink_noop$' -benchmem -count=8)
for p in adapter/slog adapter/zap adapter/zerolog; do
  (cd "$p" && go test -run '^$' -bench . -benchmem -count=5 ./...)
done
```

Key observed ranges on Apple M4:

HTTP middleware noop-sink path:

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `BenchmarkRouter_std/middleware_on_sink_noop-10` | 188.4-204.2 | 208 | 4 |
| `BenchmarkRouter_gin/middleware_on_sink_noop-10` | 207.5-211.4 | 208 | 4 |
| `BenchmarkRouter_echo/middleware_on_sink_noop-10` | 219.0-222.2 | 232 | 5 |

Selected operation paths:

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `BenchmarkOperationLifecycle-10` | 519.2-541.8 | 1288 | 12 |
| `BenchmarkOperationLifecycleLocalDirectAdd2FastSink-10` | 158.7-178.0 | 384 | 1 |
| `BenchmarkOperationLifecycleInPlaceReuseDirectAdd2FinishPreparedFastSink-10` | 72.20-75.29 | 0 | 0 |
| `BenchmarkOperationLifecyclePreparedStartInPlaceNoTimingReuseDirectAdd2FinishPreparedFastSink-10` | 34.27-36.91 | 0 | 0 |

Selected adapter fast paths:

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `BenchmarkAdapter_zerolog/write_fields_small-10` | 122.3-125.0 | 0 | 0 |
| `BenchmarkAdapter_zap/write_fields_small-10` | 413.7-422.6 | 0 | 0 |
| `BenchmarkAdapter_slog/write_fields_small-10` | 537.9-566.7 | 48 | 1 |

Interpretation:

- Small events stay in an inline typed field store, avoiding map construction and scalar boxing on sampled-out events and on adapters that implement borrowed-field fast paths.
- The std/gin/echo integrations avoid `Request.WithContext` allocation by swapping the unexported request context field in place through a guarded unsafe helper, then restoring the original context before releasing pooled storage.
- HTTP request events use pooled storage, prepared config, monotonic-only duration timing, and a guarded success fast path.
- The std integration uses pooled in-package response-writer trackers instead of `httpsnoop`, preserving optional interfaces while removing the hook/closure allocation stack from the hot path.
- In-place and prepared operation APIs are materially faster, but they are sharp hot-path tools, not the default ergonomic API.
- Borrowed-field adapter writes avoid map snapshot cost; `zerolog` and `zap` remain zero-alloc, and `slog` drops to one allocation per write by using `LogAttrs`.

The older full-report tables below are retained as historical baseline data from the February 10, 2026 run.

## Scope
- Adapters: `adapter/slog`, `adapter/zap`, `adapter/zerolog`
- Routers: `integration/std`, `integration/gin`, `integration/echo`, `integration/fiber`, `integration/fiberv3`

## Methodology
- Adapter benchmarks: `go test -run '^$' -bench . -benchmem -count=5`
- Router benchmarks: `cd bench && go test ./integration -run '^$' -bench BenchmarkRouter -benchmem -count=3`
- Router baselines now include:
  - `normal_logging_slog_noop_handler_no_middleware`
  - `normal_logging_slog_json_no_middleware`
  - `normal_logging_zap_nop_no_middleware`
  - `normal_logging_zerolog_nop_no_middleware`
- Profiles captured with `-cpuprofile` and `-memprofile`.

## Repro Commands

### Adapters (5 runs)
```bash
mkdir -p .bench/full
for p in adapter/slog adapter/zap adapter/zerolog; do
  (cd "$p" && go test -run '^$' -bench . -benchmem -count=5 ./...) \
    > "$PWD/.bench/full/${p//\//_}_bench.txt"
done
```

### Routers (3 runs, all logger baselines; centralized in `bench/integration`)
```bash
mkdir -p .bench/fair
(cd bench && go test ./integration -run '^$' -bench BenchmarkRouter -benchmem -count=3) \
  > "$PWD/.bench/fair/bench_integration_fair_all_loggers.txt"
```

### Profiles
```bash
mkdir -p .bench/full/profiles .bench/full/pprof
(cd adapter/slog && go test -run '^$' -bench 'BenchmarkAdapter_slog/write_medium_deterministic' -benchtime=5s -benchmem \
  -cpuprofile "$PWD/.bench/full/profiles/adapter_slog_cpu.prof" \
  -memprofile "$PWD/.bench/full/profiles/adapter_slog_mem.prof" ./...)

go tool pprof -top .bench/full/profiles/adapter_slog_cpu.prof
go tool pprof -top -alloc_space .bench/full/profiles/adapter_slog_mem.prof
```

## Adapter Results (5-run mean)

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `BenchmarkAdapter_zerolog/write_small-10` | 155.1 | 0 | 0.0 |
| `BenchmarkAdapter_zerolog/write_medium-10` | 353.9 | 0 | 0.0 |
| `BenchmarkAdapter_zap/write_small-10` | 552.7 | 0 | 0.0 |
| `BenchmarkAdapter_zap/write_medium-10` | 858.0 | 0 | 0.0 |
| `BenchmarkAdapter_slog/write_small-10` | 742.4 | 336 | 7.0 |
| `BenchmarkAdapter_slog/write_medium-10` | 1698.8 | 1297 | 18.0 |
| `BenchmarkAdapter_slog/write_medium_deterministic-10` | 2078.8 | 1297 | 18.0 |

## Router Results (3-run mean, fair logger baselines)

### std
| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `middleware_on_sink_noop` | 525.5 | 1616 | 21.0 |
| `normal_logging_slog_noop_handler_no_middleware` | 276.7 | 400 | 8.0 |
| `normal_logging_slog_json_no_middleware` | 604.6 | 400 | 8.0 |
| `normal_logging_zap_nop_no_middleware` | 105.2 | 464 | 5.0 |
| `normal_logging_zerolog_nop_no_middleware` | 84.4 | 208 | 4.0 |

### gin
| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `middleware_on_sink_noop` | 479.9 | 1392 | 14.0 |
| `normal_logging_slog_noop_handler_no_middleware` | 305.7 | 400 | 8.0 |
| `normal_logging_slog_json_no_middleware` | 611.0 | 400 | 8.0 |
| `normal_logging_zap_nop_no_middleware` | 130.5 | 464 | 5.0 |
| `normal_logging_zerolog_nop_no_middleware` | 97.7 | 208 | 4.0 |

### echo
| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `middleware_on_sink_noop` | 495.7 | 1472 | 15.0 |
| `normal_logging_slog_noop_handler_no_middleware` | 298.5 | 400 | 8.0 |
| `normal_logging_slog_json_no_middleware` | 605.1 | 400 | 8.0 |
| `normal_logging_zap_nop_no_middleware` | 127.9 | 464 | 5.0 |
| `normal_logging_zerolog_nop_no_middleware` | 96.6 | 208 | 4.0 |

### fiber
| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `middleware_on_sink_noop` | 4817.0 | 6226 | 27.0 |
| `normal_logging_slog_noop_handler_no_middleware` | 3155.3 | 5552 | 22.0 |
| `normal_logging_slog_json_no_middleware` | 11731.7 | 5562 | 22.0 |
| `normal_logging_zap_nop_no_middleware` | 5149.7 | 5625 | 19.0 |
| `normal_logging_zerolog_nop_no_middleware` | 5356.0 | 5370 | 18.0 |

### fiberv3
| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `middleware_on_sink_noop` | 12462.3 | 6271 | 28.0 |
| `normal_logging_slog_noop_handler_no_middleware` | 6205.0 | 5596 | 23.0 |
| `normal_logging_slog_json_no_middleware` | 6611.0 | 5595 | 23.0 |
| `normal_logging_zap_nop_no_middleware` | 3722.7 | 5656 | 20.0 |
| `normal_logging_zerolog_nop_no_middleware` | 3926.3 | 5401 | 19.0 |

## Profiling Summary

### Adapters
- `zerolog` remains fastest in adapter-only throughput.
- `zap` is second, with low overhead and zero-alloc adapter path.
- `slog` is slowest; deterministic mode adds extra sort work.

### Routers
- `std/gin/echo` profiles show major allocation share in:
  - `maps.clone` (`EventFields` shallow snapshot clone)
  - `(*Event).addKV`
  - framework response/request wrappers and final event snapshots
- `fiber/fiberv3` profiles are dominated by `App.Test` / `fasthttp` harness internals (`bufio.NewReaderSize`, `ReadResponse`) more than middleware code.
- Fiber-family benchmark runs showed noticeable variance in this environment; treat those rows as directional.

## Interpretation
- With fair no-op logger baselines, `middleware_on_sink_noop` is slower than direct no-op logging paths (expected: event/context lifecycle work).
- `middleware_on_sink_noop` is often faster than direct `slog` JSON logging, but slower than direct `zap`/`zerolog` no-op logging.
- Adapter-only ranking remains: `zerolog` > `zap` > `slog`.

## Artifacts
- Adapter raw outputs: `.bench/full/*_bench.txt`
- Router fair raw outputs: `.bench/fair/bench_integration_fair_all_loggers.txt`
- Profiles: `.bench/full/profiles/*.prof`
- Pprof tops: `.bench/full/pprof/*_top.txt`
