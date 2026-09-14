package slog

// Bridge robustness tests: nil/garbage abuse and typed-nil error
// containment.

import (
	"bytes"
	"context"
	stdslog "log/slog"
	"os"
	"strings"
	"testing"

	"github.com/happytoolin/unolog"
)

type recSink struct{ rec *unolog.Record }

func (s *recSink) Write(_ context.Context, rec *unolog.Record) { s.rec = rec }

func crashRecord(t *testing.T) *unolog.Record {
	t.Helper()
	s := &recSink{}
	rt := unolog.MustCompile(unolog.Config{Sink: s, SamplingRate: 1})
	op := unolog.Start(context.Background(), rt, unolog.OperationStart{Domain: unolog.DomainJob, Name: "j"})
	unolog.Add(op.Context(), "k", "v")
	if !op.End(nil) || s.rec == nil {
		t.Fatal("no record captured")
	}
	return s.rec
}

func TestCrashNilAbuse(t *testing.T) {
	rec := crashRecord(t)
	New(nil).Write(context.Background(), rec)
	New(nil).Write(context.Background(), nil)
	var nilSink *Sink
	nilSink.Write(context.Background(), rec)
	New(stdslog.New(stdslog.DiscardHandler)).Write(context.Background(), rec)
	New(stdslog.Default()).Write(context.Background(), rec)
}

func TestCrashTypedNilErrorField(t *testing.T) {
	var pe *os.PathError
	var buf bytes.Buffer
	s := &recSink{}
	rt := unolog.MustCompile(unolog.Config{Sink: s, SamplingRate: 1})
	op := unolog.Start(context.Background(), rt, unolog.OperationStart{Domain: unolog.DomainJob, Name: "j"})
	unolog.Add(op.Context(), "e", pe)
	unolog.Error(op.Context(), pe)
	_ = op.End(nil)
	rec := s.rec
	New(stdslog.New(stdslog.NewTextHandler(&buf, nil))).Write(context.Background(), rec)
	if !strings.Contains(buf.String(), "<nil>") {
		t.Fatalf("typed-nil error not rendered as <nil>: %s", buf.String())
	}
}
