package zapadapter

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/happytoolin/happycontext"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func emit(t *testing.T, sink hc.Sink, mutate func(ctx context.Context)) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	rt := hc.MustCompile(hc.Config{Sink: New(zap.New(core)), SamplingRate: 1})
	op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "t"})
	if mutate != nil {
		mutate(op.Context())
	}
	op.End(nil)
	return logs
}

func emitErr(t *testing.T, err error) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	rt := hc.MustCompile(hc.Config{Sink: New(zap.New(core)), SamplingRate: 1})
	op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "t"})
	op.End(&err)
	return logs
}

func TestSinkWriteMapsLevelAndMessage(t *testing.T) {
	logs := emit(t, nil, func(ctx context.Context) {
		hc.Add(ctx, "http.status", 500, "user_id", "u_1")
		hc.SetLevel(ctx, hc.LevelError)
	})

	if logs.Len() != 1 {
		t.Fatalf("expected one log entry, got %d", logs.Len())
	}
	entry := logs.All()[0]
	if entry.Level != zapcore.ErrorLevel {
		t.Fatalf("expected error level, got %v", entry.Level)
	}
	if entry.Message != hc.DefaultOperationMessage {
		t.Fatalf("expected default message, got %q", entry.Message)
	}
	if got := entry.ContextMap()["http.status"]; got != int64(500) {
		t.Fatalf("expected status field, got %v", got)
	}
	if got := entry.ContextMap()["user_id"]; got != "u_1" {
		t.Fatalf("expected user_id field, got %v", got)
	}
}

func TestSinkWriteMapsAllKnownLevels(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(ctx context.Context)
		err    error
		want   zapcore.Level
	}{
		{name: "debug", mutate: func(ctx context.Context) { hc.SetLevel(ctx, hc.LevelDebug) }, want: zapcore.InfoLevel},
		{name: "warn", mutate: func(ctx context.Context) { hc.SetLevel(ctx, hc.LevelWarn) }, want: zapcore.WarnLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs *observer.ObservedLogs
			if tt.err != nil {
				logs = emitErr(t, tt.err)
			} else {
				logs = emit(t, nil, tt.mutate)
			}
			entry := logs.All()[0]
			if entry.Level != tt.want {
				t.Fatalf("level = %v, want %v", entry.Level, tt.want)
			}
		})
	}
}

func TestSinkTypedFieldsAndOrder(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	rt := hc.MustCompile(hc.Config{Sink: New(zap.New(core)), SamplingRate: 1})
	op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "t"})
	hc.Add(op.Context(), "s", "v", "i", 7, "b", true, "d", time.Second)
	hc.Add(op.Context(), "k", "first", "k", "second")
	op.End(nil)

	entry := logs.All()[0]
	ctxMap := entry.ContextMap()
	if ctxMap["i"] != int64(7) {
		t.Fatalf("i = %v (%T)", ctxMap["i"], ctxMap["i"])
	}
	if ctxMap["b"] != true || ctxMap["s"] != "v" {
		t.Fatalf("s/b = %v/%v", ctxMap["s"], ctxMap["b"])
	}
	// duration: zap stores time.Duration natively in the field; the
	// observer's ContextMap renders it as time.Duration
	if d, ok := ctxMap["d"].(time.Duration); !ok || d != time.Second {
		t.Fatalf("d = %v (%T)", ctxMap["d"], ctxMap["d"])
	}
	if ctxMap["k"] != "second" {
		t.Fatalf("k = %v, want last write", ctxMap["k"])
	}
}

// TestSinkFloat32AndRawWireFidelity pins the v0 shapes: float32 renders
// 32-bit precision (0.1, not the widened double digits); raw bytes
// render via zap.Any (base64), never null.
func TestSinkFloat32AndRawWireFidelity(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	rt := hc.MustCompile(hc.Config{Sink: New(zap.New(core)), SamplingRate: 1})
	op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "t"})
	hc.Add(op.Context(), "f", float32(0.1))
	hc.AddRawJSON(op.Context(), "meta", []byte(`{"raw":true}`))
	op.End(nil)

	ctxMap := logs.All()[0].ContextMap()
	switch f := ctxMap["f"].(type) {
	case float32:
		if fmtFloat(float64(f)) != "0.1" {
			t.Fatalf("float32 field = %v, want 0.1", f)
		}
	case float64:
		if fmtFloat(f) != "0.1" {
			t.Fatalf("float32 field = %v, want 0.1 (32-bit rendering)", f)
		}
	default:
		t.Fatalf("float32 field = %v (%T)", ctxMap["f"], ctxMap["f"])
	}
	if got, ok := ctxMap["meta"]; !ok || got == nil {
		t.Fatalf("raw field = %v, want non-nil", got)
	}
}

func fmtFloat(f float64) string { return strconv.FormatFloat(f, 'g', -1, 32) }

func TestSinkWriteNilSafety(t *testing.T) {
	var nilSink *Sink
	nilSink.Write(context.Background(), nil)

	New(nil).Write(context.Background(), nil)
}

func TestSinkSkipsDisabledEvent(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel) // debug/info disabled
	rt := hc.MustCompile(hc.Config{Sink: New(zap.New(core)), SamplingRate: 1})
	op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "t"})
	op.End(nil) // success -> info -> filtered

	if logs.Len() != 0 {
		t.Fatal("disabled info event reached the core")
	}
}

func TestSinkConcurrentWrites(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	rt := hc.MustCompile(hc.Config{Sink: New(zap.New(core)), SamplingRate: 1})

	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "w"})
				hc.Add(op.Context(), "w", w, "i", i)
				op.End(nil)
			}
		}(w)
	}
	wg.Wait()

	if logs.Len() != 800 {
		t.Fatalf("entries = %d, want 800", logs.Len())
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }
