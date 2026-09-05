package slogadapter

// Bridge robustness tests: nil/garbage abuse and typed-nil error
// containment.

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"testing"

	hc "github.com/happytoolin/happycontext"
)

type recSink struct{ rec *hc.Record }

func (s *recSink) Write(_ context.Context, rec *hc.Record) { s.rec = rec }

func crashRecord(t *testing.T) *hc.Record {
	t.Helper()
	s := &recSink{}
	rt := hc.MustCompile(hc.Config{Sink: s, SamplingRate: 1})
	op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "j"})
	hc.Add(op.Context(), "k", "v")
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
	New(slog.New(slog.DiscardHandler)).Write(context.Background(), rec)
	New(slog.Default()).Write(context.Background(), rec)
}

func TestCrashTypedNilErrorField(t *testing.T) {
	var pe *os.PathError
	var buf bytes.Buffer
	s := &recSink{}
	rt := hc.MustCompile(hc.Config{Sink: s, SamplingRate: 1})
	op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "j"})
	hc.Add(op.Context(), "e", pe)
	hc.Error(op.Context(), pe)
	_ = op.End(nil)
	rec := s.rec
	New(slog.New(slog.NewTextHandler(&buf, nil))).Write(context.Background(), rec)
}
