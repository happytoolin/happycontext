package zerologadapter

// Agent E (adapters leg) — nil and garbage abuse against the bridge,
// including disabled loggers and every logger shape.

import (
	"context"
	"testing"

	hc "github.com/happytoolin/happycontext"
	"github.com/rs/zerolog"
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

	disabled := zerolog.New(nil).Level(zerolog.Disabled)
	New(&disabled).Write(context.Background(), rec)

	ts := zerolog.New(nil).With().Timestamp().Str("svc", "x").Logger()
	New(&ts).Write(context.Background(), rec)

	sampled := ts.Sample(&zerolog.BurstSampler{Burst: 1, Period: 1e9})
	New(&sampled).Write(context.Background(), rec)
}
