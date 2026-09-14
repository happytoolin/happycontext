package slog_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	stdslog "log/slog"

	"github.com/happytoolin/unolog"
	uslog "github.com/happytoolin/unolog/adapter/slog"
)

// ExampleNew shows the slog bridge: typed attributes in insertion
// order, errors as message strings.
func ExampleNew() {
	logger := stdslog.New(stdslog.NewTextHandler(io.Discard, nil))
	ts := unolog.NewTestSink()
	_ = logger
	rt := unolog.MustCompile(unolog.Config{Sink: uslog.New(demoLogger()), SamplingRate: 1})
	op := unolog.Start(context.Background(), rt, unolog.OperationStart{Domain: unolog.DomainJob, Name: "j"})
	unolog.Add(op.Context(), "k", 1, "rows", 42)
	op.End(nil)
	_ = ts
	// Output:
	// level=INFO msg=operation_completed k=1 rows=42 op.domain=job op.name=j duration_ms=0 op.outcome=success
}

func demoLogger() *stdslog.Logger {
	return stdslog.New(demoHandler{})
}

type demoHandler struct{}

func (demoHandler) Enabled(context.Context, stdslog.Level) bool { return true }
func (demoHandler) Handle(_ context.Context, r stdslog.Record) error {
	fmt.Printf("level=%s msg=%s", r.Level, r.Message)
	r.Attrs(func(a stdslog.Attr) bool {
		fmt.Printf(" %s=%v", a.Key, a.Value)
		return true
	})
	fmt.Println()
	return nil
}
func (h demoHandler) WithAttrs([]stdslog.Attr) stdslog.Handler { return h }
func (h demoHandler) WithGroup(string) stdslog.Handler         { return h }

var _ = errors.New
