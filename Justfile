set shell := ["zsh", "-cu"]

# Usage:
#   just bench                           # run all benchmarks
#   just bench-filter Event              # benchmark names matching Event
#   just bench-save baseline             # save snapshot to .bench/baseline.txt
#   just bench-compare baseline current  # compare snapshots with benchstat

default:
  @just --list

# Maintain clean dependencies.
tidy:
  go fmt ./...
  go mod tidy
  gofumpt -l -w .


lint:
  go vet ./...
  golangci-lint run ./...
  staticcheck ./...

test:
  go test ./...

coverage:
  go test ./... -cover
  (cd adapter/slog && go test ./... -cover)
  (cd adapter/zap && go test ./... -cover)
  (cd adapter/zerolog && go test ./... -cover)
  (cd integration/common && go test ./... -cover)
  (cd integration/std && go test ./... -cover)
  (cd integration/gin && go test ./... -cover)
  (cd integration/echo && go test ./... -cover)
  (cd integration/fiber && go test ./... -cover)
  (cd integration/fiberv3 && go test ./... -cover)
  (cd cmd/examples && go test ./... -cover)

bench:
  go test -run '^$' -bench . -benchmem ./...

bench-core:
  go test -run '^$' -bench . -benchmem ./bench

bench-nonhttp:
  go test -run '^$' -bench 'BenchmarkNonHTTP' -benchmem ./bench

bench-integrations:
  (cd integration/std && go test -run '^$' -bench . -benchmem ./...)
  (cd integration/gin && go test -run '^$' -bench . -benchmem ./...)
  (cd integration/echo && go test -run '^$' -bench . -benchmem ./...)
  (cd integration/fiber && go test -run '^$' -bench . -benchmem ./...)
  (cd integration/fiberv3 && go test -run '^$' -bench . -benchmem ./...)

bench-middleware-overhead:
  (cd integration/std && go test -run '^$' -bench 'BenchmarkRouter_std/middleware_on_sink_noop$' -benchmem ./...)
  (cd integration/gin && go test -run '^$' -bench 'BenchmarkRouter_gin/middleware_on_sink_noop$' -benchmem ./...)
  (cd integration/echo && go test -run '^$' -bench 'BenchmarkRouter_echo/middleware_on_sink_noop$' -benchmem ./...)
  (cd integration/fiber && go test -run '^$' -bench 'BenchmarkRouter_fiber/middleware_on_sink_noop$' -benchmem ./...)
  (cd integration/fiberv3 && go test -run '^$' -bench 'BenchmarkRouter_fiberv3/middleware_on_sink_noop$' -benchmem ./...)

bench-normal-logging:
  (cd integration/std && go test -run '^$' -bench 'BenchmarkRouter_std/normal_logging_no_middleware$' -benchmem ./...)
  (cd integration/gin && go test -run '^$' -bench 'BenchmarkRouter_gin/normal_logging_no_middleware$' -benchmem ./...)
  (cd integration/echo && go test -run '^$' -bench 'BenchmarkRouter_echo/normal_logging_no_middleware$' -benchmem ./...)
  (cd integration/fiber && go test -run '^$' -bench 'BenchmarkRouter_fiber/normal_logging_no_middleware$' -benchmem ./...)
  (cd integration/fiberv3 && go test -run '^$' -bench 'BenchmarkRouter_fiberv3/normal_logging_no_middleware$' -benchmem ./...)

bench-router-comparison:
  (cd integration/std && go test -run '^$' -bench 'BenchmarkRouter_std/(middleware_on_sink_noop|normal_logging_no_middleware)$' -benchmem ./...)
  (cd integration/gin && go test -run '^$' -bench 'BenchmarkRouter_gin/(middleware_on_sink_noop|normal_logging_no_middleware)$' -benchmem ./...)
  (cd integration/echo && go test -run '^$' -bench 'BenchmarkRouter_echo/(middleware_on_sink_noop|normal_logging_no_middleware)$' -benchmem ./...)
  (cd integration/fiber && go test -run '^$' -bench 'BenchmarkRouter_fiber/(middleware_on_sink_noop|normal_logging_no_middleware)$' -benchmem ./...)
  (cd integration/fiberv3 && go test -run '^$' -bench 'BenchmarkRouter_fiberv3/(middleware_on_sink_noop|normal_logging_no_middleware)$' -benchmem ./...)

bench-filter name:
  go test -run '^$' -bench '{{name}}' -benchmem ./...

bench-save name count='10' benchtime='1s':
  mkdir -p .bench
  go test -run '^$' -bench . -benchmem -count {{count}} -benchtime {{benchtime}} ./... | tee .bench/{{name}}.txt

bench-compare old new:
  go run golang.org/x/perf/cmd/benchstat@latest .bench/{{old}}.txt .bench/{{new}}.txt

bench-adapters:
  (cd adapter/slog && go test -run '^$' -bench . -benchmem ./...)
  (cd adapter/zap && go test -run '^$' -bench . -benchmem ./...)
  (cd adapter/zerolog && go test -run '^$' -bench . -benchmem ./...)

bench-all:
  @just bench-core
  @just bench-adapters
  @just bench-integrations

bench-save-all name count='10' benchtime='1s':
  mkdir -p .bench
  go test -run '^$' -bench . -benchmem -count {{count}} -benchtime {{benchtime}} ./bench | tee .bench/{{name}}-core.txt
  (cd adapter/slog && go test -run '^$' -bench . -benchmem -count {{count}} -benchtime {{benchtime}} ./... | tee ../../.bench/{{name}}-adapter-slog.txt)
  (cd adapter/zap && go test -run '^$' -bench . -benchmem -count {{count}} -benchtime {{benchtime}} ./... | tee ../../.bench/{{name}}-adapter-zap.txt)
  (cd adapter/zerolog && go test -run '^$' -bench . -benchmem -count {{count}} -benchtime {{benchtime}} ./... | tee ../../.bench/{{name}}-adapter-zerolog.txt)
  (cd integration/std && go test -run '^$' -bench . -benchmem -count {{count}} -benchtime {{benchtime}} ./... | tee ../../.bench/{{name}}-integration-std.txt)
  (cd integration/gin && go test -run '^$' -bench . -benchmem -count {{count}} -benchtime {{benchtime}} ./... | tee ../../.bench/{{name}}-integration-gin.txt)
  (cd integration/echo && go test -run '^$' -bench . -benchmem -count {{count}} -benchtime {{benchtime}} ./... | tee ../../.bench/{{name}}-integration-echo.txt)
  (cd integration/fiber && go test -run '^$' -bench . -benchmem -count {{count}} -benchtime {{benchtime}} ./... | tee ../../.bench/{{name}}-integration-fiber.txt)
  (cd integration/fiberv3 && go test -run '^$' -bench . -benchmem -count {{count}} -benchtime {{benchtime}} ./... | tee ../../.bench/{{name}}-integration-fiberv3.txt)

bench-compare-all old new:
  go run golang.org/x/perf/cmd/benchstat@latest .bench/{{old}}-core.txt .bench/{{new}}-core.txt
  go run golang.org/x/perf/cmd/benchstat@latest .bench/{{old}}-adapter-slog.txt .bench/{{new}}-adapter-slog.txt
  go run golang.org/x/perf/cmd/benchstat@latest .bench/{{old}}-adapter-zap.txt .bench/{{new}}-adapter-zap.txt
  go run golang.org/x/perf/cmd/benchstat@latest .bench/{{old}}-adapter-zerolog.txt .bench/{{new}}-adapter-zerolog.txt
  go run golang.org/x/perf/cmd/benchstat@latest .bench/{{old}}-integration-std.txt .bench/{{new}}-integration-std.txt
  go run golang.org/x/perf/cmd/benchstat@latest .bench/{{old}}-integration-gin.txt .bench/{{new}}-integration-gin.txt
  go run golang.org/x/perf/cmd/benchstat@latest .bench/{{old}}-integration-echo.txt .bench/{{new}}-integration-echo.txt
  go run golang.org/x/perf/cmd/benchstat@latest .bench/{{old}}-integration-fiber.txt .bench/{{new}}-integration-fiber.txt
  go run golang.org/x/perf/cmd/benchstat@latest .bench/{{old}}-integration-fiberv3.txt .bench/{{new}}-integration-fiberv3.txt
