package zapadapter

// Bridge robustness tests: nil/garbage abuse and typed-nil error
// containment.

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	hc "github.com/happytoolin/happycontext"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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
	New(zap.NewNop()).Write(context.Background(), rec)
	New(zap.NewExample()).Write(context.Background(), rec)
}

func TestCrashTypedNilErrorField(t *testing.T) {
	var pe *os.PathError
	s := &recSink{}
	rt := hc.MustCompile(hc.Config{Sink: s, SamplingRate: 1})
	op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "j"})
	hc.Add(op.Context(), "e", pe)
	hc.Error(op.Context(), pe)
	_ = op.End(nil)
	rec := s.rec
	var out bytes.Buffer
	zl := zap.New(zapcore.NewCore(zapcore.NewJSONEncoder(zapcore.EncoderConfig{TimeKey: "ts", LevelKey: "level", MessageKey: "msg"}), zapcore.Lock(zapcore.AddSync(&out)), zapcore.DebugLevel))
	New(zl).Write(context.Background(), rec)
	if !strings.Contains(out.String(), "<nil>") {
		t.Fatalf("typed-nil error not rendered as <nil>: %s", out.String())
	}
}
