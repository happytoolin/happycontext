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
  go fix ./...
  go mod tidy
  gofumpt -l -w .


lint:
  go vet ./...
  golangci-lint run ./...
  staticcheck ./...

test:
  while IFS= read -r modfile; do \
    moddir="$(dirname "$modfile")"; \
    echo "== testing $moddir =="; \
    (cd "$moddir" && go test ./...); \
  done < <(printf '%s\n' go.mod && git ls-files '**/go.mod')

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
  (cd integration/worker && go test ./... -cover)
  (cd cmd/examples && go test ./... -cover)

bench:
  (cd benches && go test -run '^$' -bench . -benchmem ./...)

bench-core:
  (cd benches && go test -run '^$' -bench 'Benchmark(Event|Commit|JSON|NonHTTP|Operation|RateSampler)' -benchmem .)

bench-nonhttp:
  (cd benches && go test -run '^$' -bench '^BenchmarkNonHTTP' -benchmem .)

bench-routers:
  (cd benches && go test -run '^$' -bench '^BenchmarkRouter' -benchmem .)

bench-middleware-overhead:
  (cd benches && go test -run '^$' -bench '^BenchmarkRouter.*/middleware_on_sink_noop$' -benchmem .)

bench-normal-logging:
  (cd benches && go test -run '^$' -bench '^BenchmarkRouter.*/normal_logging_.*' -benchmem .)

bench-router-comparison:
  (cd benches && go test -run '^$' -bench '^BenchmarkRouter.*/(middleware_on_sink_noop|normal_logging_.*)' -benchmem .)

bench-filter name:
  (cd benches && go test -run '^$' -bench '{{name}}' -benchmem ./...)

bench-save name count='10' benchtime='1s':
  mkdir -p .bench
  (cd benches && go test -run '^$' -bench . -benchmem -count {{count}} -benchtime {{benchtime}} ./...) | tee .bench/{{name}}.txt

bench-compare old new:
  go run golang.org/x/perf/cmd/benchstat@latest .bench/{{old}}.txt .bench/{{new}}.txt

bench-adapters:
  (cd benches && go test -run '^$' -bench '^BenchmarkAdapter' -benchmem .)
