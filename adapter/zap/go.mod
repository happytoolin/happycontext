module github.com/happytoolin/unolog/adapter/zap

go 1.25.0

require (
	github.com/happytoolin/unolog v0.5.0 // x-release-please-version
	go.uber.org/zap v1.27.1
)

require (
	go.uber.org/goleak v1.3.0
	go.uber.org/multierr v1.10.0 // indirect
)

replace github.com/happytoolin/unolog => ../..
