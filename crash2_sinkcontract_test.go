package hc

// Agent J — sink contract edges: the documented benign race on
// Record.Encoded(), first-encode-after-recycle, fanout sinks, and
// encoding inside Write while stragglers fire.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// The Encoded() contract: concurrent callers race benignly and share
// the winner's buffer. Pin it hard under -race: 16 goroutines, all
// must return the same bytes.
func TestCrashConcurrentEncoded(t *testing.T) {
	fields := make([]Field, 64)
	for i := range fields {
		fields[i] = fieldOf(fmt.Sprintf("k%d", i), i)
	}
	rec := recOf(LevelInfo, "m", fields...)

	const n = 16
	const iters = 50
	var wg sync.WaitGroup
	lines := make([][]byte, n)
	for g := range n {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for range iters {
				lines[g] = rec.Encoded()
			}
		}(g)
	}
	wg.Wait()
	for g := 1; g < n; g++ {
		if string(lines[g]) != string(lines[0]) {
			t.Fatalf("goroutine %d got different bytes", g)
		}
	}
	if !json.Valid(lines[0]) {
		t.Fatalf("line invalid: %s", lines[0])
	}
}

// A sink that encodes for the first time AFTER the event was recycled
// violates the retention contract — characterize: no panic, and the
// result is either the original line or another request's line (the
// documented hazard), never memory unsafety.
func TestCrashFirstEncodeAfterRecycle(t *testing.T) {
	rt := MustCompile(Config{Sink: NewJSONSink(&strings.Builder{}), SamplingRate: 1})
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "first"})
	Add(op.Context(), "who", "first")
	// Do NOT End through a capturing sink; grab the record another way.
	sink := &recordGrabber{}
	rt2 := MustCompile(Config{Sink: sink, SamplingRate: 1})
	op2 := Start(context.Background(), rt2, OperationStart{Domain: DomainJob, Name: "first"})
	Add(op2.Context(), "who", "first")
	_ = op2.End(nil)
	_ = op.End(nil)

	// Recycle a few events through the pool.
	for i := range 8 {
		o := Start(context.Background(), rt2, OperationStart{Domain: DomainJob, Name: "later"})
		Add(o.Context(), "who", fmt.Sprintf("later-%d", i))
		_ = o.End(nil)
	}

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("late Encoded panicked: %v", r)
			}
		}()
		line := sink.rec.Encoded()
		_ = json.Valid(line) // may be another request's line: the hazard
	}()
}

type recordGrabber struct {
	rec *Record
}

func (g *recordGrabber) Write(_ context.Context, rec *Record) { g.rec = rec }

// Fanout: one sink fanning out to several downstream sinks
// concurrently — the Sink contract requires the fanout itself to be
// safe, and the first-party sinks it calls must be too.
type fanoutSink struct {
	inner []Sink
}

func (f *fanoutSink) Write(ctx context.Context, rec *Record) {
	var wg sync.WaitGroup
	for _, s := range f.inner {
		wg.Add(1)
		go func(s Sink) {
			defer wg.Done()
			s.Write(ctx, rec)
		}(s)
	}
	wg.Wait()
}

func TestCrashFanoutSinks(t *testing.T) {
	var buf strings.Builder
	ts := NewTestSink()
	fan := &fanoutSink{inner: []Sink{NewJSONSink(&buf), ts, NewJSONSink(&strings.Builder{})}}
	rt := MustCompile(Config{Sink: fan, SamplingRate: 1})

	var wg sync.WaitGroup
	for w := range 8 {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := range 25 {
				op := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "request"})
				Add(op.Context(), "w", w, "i", i)
				_ = op.End(nil)
			}
		}(w)
	}
	wg.Wait()

	if got, want := len(ts.Events()), 200; got != want {
		t.Fatalf("captured = %d, want %d", got, want)
	}
	for _, ln := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if ln == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatalf("fanout line unparseable: %v: %s", err, ln)
		}
	}
}

// Encoding inside Write while stragglers hammer the (sealed) WAL:
// post-seal writes are no-ops, so the encoded line is stable.
func TestCrashEncodeDuringStragglerFire(t *testing.T) {
	sink := &encodingStragglerSink{}
	rt := MustCompile(Config{Sink: sink, SamplingRate: 1})
	op := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "request"})
	Add(op.Context(), "stable", "v")
	_ = op.End(nil)
	before := string(sink.rec.Encoded()) // cached now

	// Stragglers fire after the seal: no-op writes, cached line stable.
	for i := range 100 {
		Add(op.Context(), fmt.Sprintf("straggler-%d", i), i)
	}
	if after := string(sink.rec.Encoded()); after != before {
		t.Fatalf("encoded line changed across straggler fire:\n%s\n%s", before, after)
	}
}

type encodingStragglerSink struct {
	rec *Record
}

func (s *encodingStragglerSink) Write(_ context.Context, rec *Record) {
	s.rec = rec
	rec.Encoded() // cache the line while the record is valid
}
