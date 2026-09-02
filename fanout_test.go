package hc

// P8.3 multi-sink fan-out (dst-research §8.3): a test-only FanoutSink
// that writes each record to N sinks, one of which panics on a
// schedule. The other sinks must still receive the record, the record
// must not be corrupted, and the pool must stay clean. The core's
// emit path calls sink.Write once — fan-out happens INSIDE one sink,
// so a panicking member must not prevent the remaining members from
// writing (and must not corrupt the shared pool).

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// FanoutSink writes every record to each member sink in order. If a
// member panics, the remaining members still receive the record and
// the panic propagates to the caller (who decides what to do with it).
type FanoutSink struct {
	sinks []Sink
}

// NewFanoutSink creates a fan-out over the given sinks.
func NewFanoutSink(sinks ...Sink) *FanoutSink {
	return &FanoutSink{sinks: sinks}
}

// Write delivers the record to every member sink, continuing past
// panicking members (their panic is re-raised after the fan-out so the
// caller sees the first failure).
func (f *FanoutSink) Write(ctx context.Context, rec *Record) {
	if f == nil {
		return
	}
	var firstPanic any
	for _, s := range f.sinks {
		func() {
			defer func() {
				if r := recover(); r != nil && firstPanic == nil {
					firstPanic = r
				}
			}()
			s.Write(ctx, rec)
		}()
	}
	if firstPanic != nil {
		panic(firstPanic)
	}
}

var _ Sink = (*FanoutSink)(nil)

// captureSink records every record it receives (deep copy via the
// shared TestSink machinery).
type fanCaptureSink struct {
	mu     sync.Mutex
	events []CapturedEvent
}

func (s *fanCaptureSink) Write(_ context.Context, rec *Record) {
	if rec == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, CapturedEvent{
		level:   rec.Level(),
		message: rec.Message(),
		fields:  copyFields(rec.Fields()),
	})
}

func (s *fanCaptureSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

func (s *fanCaptureSink) last() CapturedEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.events[len(s.events)-1]
}

// panicSink panics on Write.
type panicSink struct{}

func (panicSink) Write(context.Context, *Record) { panic("sink exploded") }

// TestFanoutSinkPanicIsolation: one panicking member must not stop the
// others from receiving the record.
func TestFanoutSinkPanicIsolation(t *testing.T) {
	for _, n := range []int{2, 4, 8} {
		for _, panicIdx := range []int{0, n / 2, n - 1} {
			t.Run(fmt.Sprintf("n=%d panic=%d", n, panicIdx), func(t *testing.T) {
				captures := make([]*fanCaptureSink, n)
				sinks := make([]Sink, n)
				for i := range sinks {
					captures[i] = &fanCaptureSink{}
					sinks[i] = captures[i]
				}
				sinks[panicIdx] = panicSink{}
				ts := NewFanoutSink(sinks...)
				rt := MustCompile(Config{Sink: ts, SamplingRate: 1})
				op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "fan"})
				Add(op.Context(), "k", "v")

				var escaped any
				func() {
					defer func() { escaped = recover() }()
					op.End(nil)
				}()
				if escaped == nil {
					t.Fatal("the panicking sink's panic did not escape")
				}
				// every non-panicking member received the record
				for i, c := range captures {
					if i == panicIdx {
						continue // this slot holds the panicking sink
					}
					if c.count() != 1 {
						t.Fatalf("sink %d captured %d events", i, c.count())
					}
					if v, _ := c.last().Lookup("k"); v != "v" {
						t.Fatalf("sink %d event corrupted", i)
					}
				}
				// pool clean after the panic
				ok := &fanCaptureSink{}
				rt2 := MustCompile(Config{Sink: ok, SamplingRate: 1})
				op2 := Start(context.Background(), rt2, OperationStart{Domain: DomainJob, Name: "after"})
				Add(op2.Context(), "clean", true)
				op2.End(nil)
				if ok.count() != 1 {
					t.Fatal("pool corrupted after the panicking fan-out")
				}
			})
		}
	}
}

// TestFanoutAllPanicking: when every member panics the caller sees the
// first panic, and the request still does not corrupt the pool.
func TestFanoutAllPanicking(t *testing.T) {
	for _, n := range []int{2, 4} {
		sinks := make([]Sink, n)
		for i := range sinks {
			sinks[i] = panicSink{}
		}
		rt := MustCompile(Config{Sink: NewFanoutSink(sinks...), SamplingRate: 1})
		op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "all-panic"})
		Add(op.Context(), "k", 1)
		var escaped any
		func() {
			defer func() { escaped = recover() }()
			op.End(nil)
		}()
		if escaped == nil || escaped != "sink exploded" {
			t.Fatalf("escaped %v", escaped)
		}
		// pool clean: the next request works
		ok := &fanCaptureSink{}
		rt2 := MustCompile(Config{Sink: ok, SamplingRate: 1})
		op2 := Start(context.Background(), rt2, OperationStart{Domain: DomainJob, Name: "after"})
		op2.End(nil)
		if ok.count() != 1 {
			t.Fatal("pool corrupted after all-panic fan-out")
		}
	}
}

// TestFanoutRetainingSink: a member that retains the Record past Write
// must copy it (amendment 9) — a buggy retaining member would observe
// recycled memory, which the pool-safety tests pin elsewhere; here we
// pin that the fan-out passes the SAME record view to every member.
func TestFanoutSinkOrder(t *testing.T) {
	a, b := &fanCaptureSink{}, &fanCaptureSink{}
	rt := MustCompile(Config{Sink: NewFanoutSink(a, b), SamplingRate: 1})
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "order"})
	Add(op.Context(), "k", "v")
	op.End(nil)
	la, lb := a.last(), b.last()
	if la.Message() != lb.Message() || la.Level() != lb.Level() {
		t.Fatalf("members disagree: %+v vs %+v", la, lb)
	}
	if !strings.Contains(fmt.Sprint(la.Lookup("k")), "v") {
		t.Fatalf("first member lost the field")
	}
	if _, ok := lb.Lookup("k"); !ok {
		t.Fatal("second member lost the field")
	}
}
