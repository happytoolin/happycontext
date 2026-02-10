# happycontext Benchmark & Profiling Report

Date: February 10, 2026
Machine: Apple M4 (darwin/arm64)

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
  - `context.WithValue` / `Request.WithContext`
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
