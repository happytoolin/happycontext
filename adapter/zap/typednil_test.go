package zapadapter

// Typed-nil error fields must render "<nil>", not panic the bridge.

import (
	"bytes"
	"context"
	"os"
	"testing"

	hc "github.com/happytoolin/happycontext"
	"go.uber.org/zap"
)

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
	_ = buf
	New(zap.NewExample()).Write(context.Background(), rec)
}
