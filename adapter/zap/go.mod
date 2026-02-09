module github.com/happytoolin/hlog/adapter/zap

go 1.24

require (
	github.com/happytoolin/hlog v0.0.0
	go.uber.org/zap v1.27.1
)

require go.uber.org/multierr v1.10.0 // indirect

replace github.com/happytoolin/hlog => ../../
