package slogadapter_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	hc "github.com/happytoolin/happycontext"
	sloghc "github.com/happytoolin/happycontext/adapter/slog"
)

// ExampleNew shows the slog bridge: typed attributes in insertion
// order, errors as message strings.
func ExampleNew() {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ts := hc.NewTestSink()
	_ = logger
	rt := hc.MustCompile(hc.Config{Sink: sloghc.New(demoLogger()), SamplingRate: 1})
	op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "j"})
	hc.Add(op.Context(), "k", 1, "rows", 42)
	op.End(nil)
	_ = ts
	// Output:
	// level=INFO msg=operation_completed k=1 rows=42 op.domain=job op.name=j duration_ms=0 op.outcome=success
}

func demoLogger() *slog.Logger {
	return slog.New(demoHandler{})
}

type demoHandler struct{}

func (demoHandler) Enabled(context.Context, slog.Level) bool { return true }
func (demoHandler) Handle(_ context.Context, r slog.Record) error {
	fmt.Printf("level=%s msg=%s", r.Level, r.Message)
	r.Attrs(func(a slog.Attr) bool {
		fmt.Printf(" %s=%v", a.Key, a.Value)
		return true
	})
	fmt.Println()
	return nil
}
func (h demoHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h demoHandler) WithGroup(string) slog.Handler      { return h }

var _ = errors.New
