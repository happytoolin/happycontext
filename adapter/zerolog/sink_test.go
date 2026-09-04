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
	if payload["k"] != "second" || strings.Count(buf.String(), `"k":`) != 1 {
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

	var zl zerolog.Logger // zero-value logger: no writer, nothing emits
	New(&zl).Write(context.Background(), nil)
}

// captureSink retains the *hc.Record handed to Write so a test can
// drive the bridge with an already-built record (the bridge-only shape).
type captureSink struct{ recs []*hc.Record }

func (c *captureSink) Write(_ context.Context, rec *hc.Record) { c.recs = append(c.recs, rec) }

func bridgeRecord(t *testing.T, mutate func(ctx context.Context)) *hc.Record {
	t.Helper()
	cap := &captureSink{}
	rt := hc.MustCompile(hc.Config{Sink: cap, SamplingRate: 1})
	op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "t"})
	if mutate != nil {
		mutate(op.Context())
	}
	op.End(nil)
	if len(cap.recs) != 1 {
		t.Fatalf("captured %d records", len(cap.recs))
	}
	return cap.recs[0]
}

// TestSinkFastPathServesCanonicalLine pins the direct-serve fast path:
// for a plain zerolog.New(w) logger the bridge writes the record's own
// pre-encoded canonical line byte-for-byte — one Write call, no typed
// constructors, no zerolog-assembled envelope.
func TestSinkFastPathServesCanonicalLine(t *testing.T) {
	rec := bridgeRecord(t, func(ctx context.Context) {
		hc.Add(ctx, "s", "v", "i", 7, "b", true, "d", 1500*time.Millisecond)
	})

	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	New(&logger).Write(context.Background(), rec)

	if got, want := buf.Bytes(), rec.Encoded(); !bytes.Equal(got, want) {
		t.Fatalf("bridge output differs from the canonical line:\ngot  %q\nwant %q", got, want)
	}
	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &payload); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	for k, want := range map[string]any{"level": "info", "s": "v", "i": float64(7), "b": true, "d": float64(1500)} {
		if payload[k] != want {
			t.Fatalf("%s = %v, want %v", k, payload[k], want)
		}
	}
}

// TestSinkFastPathRespectsLoggerLevel pins the level gate: the fast
// path must not write when the logger's threshold filters the record's
// level (the semantics of event.Enabled() on the typed path).
func TestSinkFastPathRespectsLoggerLevel(t *testing.T) {
	info := bridgeRecord(t, nil) // success → info
	errRec := bridgeRecord(t, func(ctx context.Context) { hc.SetLevel(ctx, hc.LevelError) })

	var buf bytes.Buffer
	logger := zerolog.New(&buf).Level(zerolog.WarnLevel)
	sink := New(&logger)

	sink.Write(context.Background(), info)
	if buf.Len() != 0 {
		t.Fatalf("info record crossed a warn threshold: %q", buf.String())
	}
	sink.Write(context.Background(), errRec)
	if buf.Len() == 0 {
		t.Fatal("error record filtered by a warn threshold")
	}
	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &payload); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if payload["level"] != "error" {
		t.Fatalf("level = %v", payload["level"])
	}
}

// TestSinkDisabledLoggerEmitsNothing: a Disabled logger filters every
// level on the fast path, like event.Enabled() == false.
func TestSinkDisabledLoggerEmitsNothing(t *testing.T) {
	rec := bridgeRecord(t, func(ctx context.Context) { hc.SetLevel(ctx, hc.LevelError) })
	var buf bytes.Buffer
	logger := zerolog.New(&buf).Level(zerolog.Disabled)
	New(&logger).Write(context.Background(), rec)
	if buf.Len() != 0 {
		t.Fatalf("disabled logger wrote %q", buf.String())
	}
}

// TestSinkZeroValueLoggerEmitsNothing: a zero-value *zerolog.Logger has
// no writer; the sink must drop records without panicking.
func TestSinkZeroValueLoggerEmitsNothing(t *testing.T) {
	rec := bridgeRecord(t, nil)
	var logger zerolog.Logger
	New(&logger).Write(context.Background(), rec) // no observable output: must not panic
}

// TestSinkEnrichedLoggerKeepsNativeAugmentation: loggers carrying
// zerolog context fields, hooks, or samplers take the typed path, so
// their native augmentation survives the bridge.
func TestSinkEnrichedLoggerKeepsNativeAugmentation(t *testing.T) {
	rec := bridgeRecord(t, func(ctx context.Context) {
		hc.Add(ctx, "user_id", "u_1")
	})

	t.Run("context_fields", func(t *testing.T) {
		var buf bytes.Buffer
		logger := zerolog.New(&buf).With().Str("svc", "payments").Logger()
		New(&logger).Write(context.Background(), rec)
		payload := lastPayload(t, &buf)
		if payload["svc"] != "payments" {
			t.Fatalf("context field dropped: %v", payload)
		}
		if payload["user_id"] != "u_1" {
			t.Fatalf("record field missing: %v", payload)
		}
	})

	t.Run("sampler", func(t *testing.T) {
		var buf bytes.Buffer
		logger := zerolog.New(&buf).Sample(&zerolog.BasicSampler{N: 1}) // keeps everything
		New(&logger).Write(context.Background(), rec)
		if buf.Len() == 0 {
			t.Fatal("sampled logger dropped the record")
		}
	})

	t.Run("hook", func(t *testing.T) {
		var buf bytes.Buffer
		var ran bool
		logger := zerolog.New(&buf).Hook(zerolog.HookFunc(func(*zerolog.Event, zerolog.Level, string) {
			ran = true
		}))
		New(&logger).Write(context.Background(), rec)
		if !ran {
			t.Fatal("hook did not run: enriched logger bypassed the typed path")
		}
	})
}

// TestSinkCustomizedFieldNamesFallsBackToTypedPath pins the F1 gate:
// when zerolog's member-name globals are customized, the bridge must
// not serve the canonical line (whose envelope members are always
// "level"/"message"/"time") — the typed path emits the customized
// names through zerolog's own constructors.
func TestSinkCustomizedFieldNamesFallsBackToTypedPath(t *testing.T) {
	levelName, timeName, messageName := zerolog.LevelFieldName, zerolog.TimestampFieldName, zerolog.MessageFieldName
	zerolog.LevelFieldName, zerolog.TimestampFieldName, zerolog.MessageFieldName = "lvl", "ts", "msg"
	t.Cleanup(func() {
		zerolog.LevelFieldName, zerolog.TimestampFieldName, zerolog.MessageFieldName = levelName, timeName, messageName
	})

	rec := bridgeRecord(t, func(ctx context.Context) { hc.Add(ctx, "k", "v") })
	var buf bytes.Buffer
	logger := zerolog.New(&buf) // plain logger: the fast path would serve it
	New(&logger).Write(context.Background(), rec)

	if buf.Len() == 0 {
		t.Fatal("record dropped")
	}
	payload := lastPayload(t, &buf)
	if payload["lvl"] != "info" {
		t.Fatalf("lvl = %v, want the customized level member", payload["lvl"])
	}
	if payload["msg"] != rec.Message() {
		t.Fatalf("msg = %v, want the customized message member", payload["msg"])
	}
	if _, ok := payload["level"]; ok {
		t.Fatalf("canonical %q member leaked onto a customized pipeline: %v", "level", payload)
	}
	if _, ok := payload["message"]; ok {
		t.Fatalf("canonical %q member leaked onto a customized pipeline: %v", "message", payload)
	}
	if payload["k"] != "v" {
		t.Fatalf("record field missing: %v", payload)
	}
}

// TestSinkTypedPathStampsRecordCompletionTime pins the F6 symmetry:
// the enriched-logger typed path stamps the record's own completion
// time (rec.Time()) — the instant the canonical line carries — rather
// than a fresh write-time read.
func TestSinkTypedPathStampsRecordCompletionTime(t *testing.T) {
	rec := bridgeRecord(t, func(ctx context.Context) { hc.Add(ctx, "k", "v") })

	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Str("svc", "payments").Logger() // enriched: typed path
	New(&logger).Write(context.Background(), rec)

	payload := lastPayload(t, &buf)
	s, ok := payload["time"].(string)
	if !ok {
		t.Fatalf("time = %v, want RFC3339 string", payload["time"])
	}
	got, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("time %q not RFC3339: %v", s, err)
	}
	// Both the canonical line and zerolog's TimeFieldFormat render
	// RFC3339 seconds precision, so compare at that granularity.
	if want := rec.Time().Truncate(time.Second); !got.Equal(want) {
		t.Fatalf("typed path stamped %v, want the record's completion time %v", got, want)
	}
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
