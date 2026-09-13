module github.com/happytoolin/happycontext/adapter/zerolog

go 1.25.0

require (
	github.com/happytoolin/happycontext v0.5.0 // x-release-please-version
	github.com/rs/zerolog v1.35.1
)

require (
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	go.uber.org/goleak v1.3.0
	golang.org/x/sys v0.39.0 // indirect
)

replace github.com/happytoolin/happycontext => ../..
