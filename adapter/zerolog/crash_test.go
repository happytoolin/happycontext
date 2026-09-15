package zerolog

// Bridge robustness tests: nil/garbage abuse and typed-nil error
// containment.

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/happytoolin/unolog"
	gozerolog "github.com/rs/zerolog"
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

	disabled := gozerolog.New(nil).Level(gozerolog.Disabled)
	New(&disabled).Write(context.Background(), rec)

	ts := gozerolog.New(nil).With().Timestamp().Str("svc", "x").Logger()
	New(&ts).Write(context.Background(), rec)

	sampled := ts.Sample(&gozerolog.BurstSampler{Burst: 1, Period: 1e9})
	New(&sampled).Write(context.Background(), rec)
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
	zl := gozerolog.New(&buf)
	New(&zl).Write(context.Background(), rec)
	if !strings.Contains(buf.String(), `"<nil>"`) {
		t.Fatalf("typed-nil error not rendered as <nil>: %s", buf.String())
	}
}
