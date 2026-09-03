package zerologadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/happytoolin/happycontext"
	"github.com/rs/zerolog"
)

func emit(t *testing.T, buf *bytes.Buffer, mutate func(ctx context.Context)) {
	t.Helper()
	logger := zerolog.New(buf)
	rt := hc.MustCompile(hc.Config{Sink: New(&logger), SamplingRate: 1})
	op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "t"})
	if mutate != nil {
		mutate(op.Context())
	}
	op.End(nil)
}

func lastPayload(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	line := bytes.TrimRight(buf.Bytes(), "\n")
	lines := strings.Split(string(line), "\n")
	var payload map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &payload); err != nil {
		t.Fatalf("invalid JSON %q: %v", line, err)
	}
	return payload
}

func TestSinkWriteMapsLevelAndFields(t *testing.T) {
	var buf bytes.Buffer
	emit(t, &buf, func(ctx context.Context) {
		hc.Add(ctx, "http.status", 500, "user_id", "u_1")
		hc.SetLevel(ctx, hc.LevelError)
	})

	payload := lastPayload(t, &buf)
	if payload["level"] != "error" {
		t.Fatalf("level = %v", payload["level"])
	}
	if payload["message"] != hc.DefaultOperationMessage {
		t.Fatalf("message = %q", payload["message"])
	}
	if payload["user_id"] != "u_1" {
		t.Fatalf("user_id = %v", payload["user_id"])
	}
	if payload["http.status"] != float64(500) {
		t.Fatalf("http.status = %v", payload["http.status"])
	}
	if _, err := time.Parse(time.RFC3339, payload["time"].(string)); err != nil {
		t.Fatalf("time not RFC3339: %v", payload["time"])
	}
}

func TestSinkWriteMapsAllKnownLevels(t *testing.T) {
	cases := map[string]struct {
		mutate func(ctx context.Context)
		err    error
		want   string
	}{
		"debug": {mutate: func(ctx context.Context) { hc.SetLevel(ctx, hc.LevelDebug) }, want: "info"}, // floor never lowers
		"warn":  {mutate: func(ctx context.Context) { hc.SetLevel(ctx, hc.LevelWarn) }, want: "warn"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			emit(t, &buf, c.mutate)
			if payload := lastPayload(t, &buf); payload["level"] != c.want {
				t.Fatalf("level = %v, want %v", payload["level"], c.want)
			}
		})
	}
	t.Run("error", func(t *testing.T) {
		var buf bytes.Buffer
		var err error = errBoom{}
		logger := zerolog.New(&buf)
		rt := hc.MustCompile(hc.Config{Sink: New(&logger), SamplingRate: 1})
		op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "t"})
		op.End(&err)
		if payload := lastPayload(t, &buf); payload["level"] != "error" {
			t.Fatalf("level = %v", payload["level"])
		}
	})
}

func TestSinkTypedFieldsAndDedupe(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	emit(t, &buf, func(ctx context.Context) {
		hc.Add(ctx, "s", "v", "i", 7, "b", true, "t", now, "d", 1500*time.Millisecond)
		hc.Add(ctx, "k", "first", "k", "second")
		hc.AddRawJSON(ctx, "meta", []byte(`{"raw":true}`))
	})

	payload := lastPayload(t, &buf)
	if payload["i"] != float64(7) || payload["b"] != true || payload["s"] != "v" {
		t.Fatalf("scalars = %v %v %v", payload["i"], payload["b"], payload["s"])
	}
	if payload["t"] != "2026-09-01T10:00:00Z" {
		t.Fatalf("t = %v", payload["t"])
	}
	if payload["d"] != 1500.0 { // zerolog Dur default: float ms
		t.Fatalf("d = %v", payload["d"])
	}
	if payload["k"] != "second" || strings.Count(string(buf.Bytes()), `"k":`) != 1 {
		t.Fatalf("dedupe broken: k = %v", payload["k"])
	}
	if raw, ok := payload["meta"].(map[string]any); !ok || raw["raw"] != true {
		t.Fatalf("raw json = %v", payload["meta"])
	}
}

// TestSinkFloat32WireFidelity pins the 32-bit rendering (0.1, not the
// widened double digits).
func TestSinkFloat32WireFidelity(t *testing.T) {
	var buf bytes.Buffer
	emit(t, &buf, func(ctx context.Context) {
		hc.Add(ctx, "f", float32(0.1))
	})
	payload := lastPayload(t, &buf)
	if payload["f"] != 0.1 {
		t.Fatalf("float32 = %v, want 0.1", payload["f"])
	}
	if !strings.Contains(buf.String(), `"f":0.1`) {
		t.Fatalf("wire shows widened digits: %s", buf.String())
	}
}

func TestSinkWriteNilSafety(t *testing.T) {
	var nilSink *Sink
	nilSink.Write(context.Background(), nil)

	New(nil).Write(context.Background(), nil)
}

func TestSinkConcurrentWrites(t *testing.T) {
	var mu sync.Mutex
	var buf bytes.Buffer
	logger := zerolog.New(lockedWriter{mu: &mu, buf: &buf})
	rt := hc.MustCompile(hc.Config{Sink: New(&logger), SamplingRate: 1})

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

	mu.Lock()
	defer mu.Unlock()
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 800 {
		t.Fatalf("lines = %d, want 800", len(lines))
	}
	for _, ln := range lines {
		var payload map[string]any
		if err := json.Unmarshal([]byte(ln), &payload); err != nil {
			t.Fatalf("corrupt line: %q", ln)
		}
	}
}

type lockedWriter struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }
