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
