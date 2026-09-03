package hc_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	hc "github.com/happytoolin/happycontext"
)

// printSink renders records deterministically for the output-checked
// examples: sorted field keys, values as JSON.
type printSink struct {
	w *os.File
}

func (p printSink) Write(_ context.Context, rec *hc.Record) {
	keys := make([]string, 0, len(rec.Fields()))
	seen := map[string]bool{}
	for _, f := range rec.Fields() {
		if seen[f.Key()] {
			continue
		}
		seen[f.Key()] = true
		keys = append(keys, f.Key())
	}
	sort.Strings(keys)
	fmt.Fprintf(p.w, "%s %s", rec.Level(), rec.Message())
	for _, k := range keys {
		var v any
		for _, f := range rec.Fields() {
			if f.Key() == k {
				v, _ = rec.Lookup(k)
			}
		}
		if k == "duration_ms" {
			continue // deterministic output: skip timing
		}
		b, _ := json.Marshal(v)
		fmt.Fprintf(p.w, " %s=%s", k, b)
	}
	fmt.Fprintln(p.w)
}

// ExampleCompile shows the compile-once contract: bad configuration is
// a construction-time error wrapping sentinel values.
func ExampleCompile() {
	_, err := hc.Compile(hc.Config{SamplingRate: 1.5})
	fmt.Println(err)
	fmt.Println(errors.Is(err, hc.ErrInvalidRate))

	rt, err := hc.Compile(hc.Config{Sink: printSink{os.Stdout}, SamplingRate: 1})
	if err != nil {
		fmt.Println("bad config:", err)
		return
	}
	hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "j"}).End(nil)
	// Output:
	// hc: sampling rate 1.5: hc: invalid rate
	// true
	// INFO operation_completed op.domain="job" op.name="j" op.outcome="success"
}

// ExampleMustCompile is the literal-config idiom for main.
func ExampleMustCompile() {
	rt := hc.MustCompile(hc.Config{
		Sink:         printSink{os.Stdout},
		SamplingRate: 1,
		Message:      "done",
	})
	op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "digest"})
	op.End(nil)
	// Output:
	// INFO done op.domain="job" op.name="digest" op.outcome="success"
}

// ExampleStart shows the request lifecycle: Start attaches the WAL,
// the hc.Add helpers annotate, the deferred End commits exactly once.
func ExampleStart() {
	rt := hc.MustCompile(hc.Config{Sink: printSink{os.Stdout}, SamplingRate: 1})

	func() (err error) {
		op := hc.Start(context.Background(), rt, hc.OperationStart{
			Domain:      hc.DomainJob,
			Name:        "import",
			ID:          "job_1",
			Attempt:     2,
			MaxAttempts: 3,
		})
		defer op.End(&err) // direct defer: captures errors and panics

		hc.Add(op.Context(), "rows", 42, "source", "queue")
		hc.AddRawJSON(op.Context(), "meta", []byte(`{"batch":true}`))
		return errors.New("row 17 failed")
	}()
	// Output:
	// ERROR operation_completed error={"message":"row 17 failed","type":"*errors.errorString"} meta="eyJiYXRjaCI6dHJ1ZX0=" op.attempt=2 op.domain="job" op.id="job_1" op.max_attempts=3 op.name="import" op.outcome="failure" rows=42 source="queue"
}

// ExampleOperation_End demonstrates the deferred idiom and the
// emitted flag; a second End call is a no-op returning the first
// result.
func ExampleOperation_End() {
	rt := hc.MustCompile(hc.Config{Sink: printSink{os.Stdout}, SamplingRate: 1})
	var err error
	op := hc.Start(context.Background(), rt, hc.OperationStart{})
	emitted := op.End(&err)
	fmt.Println("emitted:", emitted, "again:", op.End(&err))
	// Output:
	// INFO operation_completed op.domain="operation" op.name="operation" op.outcome="success"
	// emitted: true again: true
}

// ExampleAdd shows annotation-style fields: strings, ints, nested
// values, and the kv variadic.
func ExampleAdd() {
	rt := hc.MustCompile(hc.Config{Sink: printSink{os.Stdout}, SamplingRate: 1})
	op := hc.Start(context.Background(), rt, hc.OperationStart{})
	ctx := op.Context()
	hc.Add(ctx, "user_id", "u_1", "attempt", 2)
	hc.Add(ctx, "cart", map[string]any{"items": 3})
	hc.SetRoute(ctx, "/orders/:id")
	op.End(nil)
	// Output:
	// INFO operation_completed attempt=2 cart={"items":3} http.route="/orders/:id" op.domain="operation" op.name="operation" op.outcome="success" user_id="u_1"
}

// ExampleError records a structured error; failures are never sampled
// away, even under NeverSampler.
func ExampleError() {
	rt := hc.MustCompile(hc.Config{
		Sink:    printSink{os.Stdout},
		Sampler: func(hc.SampleInput) bool { return false },
	})
	var err error = errors.New("db timeout")
	op := hc.Start(context.Background(), rt, hc.OperationStart{})
	op.End(&err) // still emitted: error bypass is structural
	// Output:
	// ERROR operation_completed error={"message":"db timeout","type":"*errors.errorString"} op.domain="operation" op.name="operation" op.outcome="failure"
}

// ExampleNewJSONSink is the zero-dependency output path: canonical
// JSON lines with a single Write per event. The line is truncated at
// the timestamp for determinism.
func ExampleNewJSONSink() {
	var buf strings.Builder
	rt := hc.MustCompile(hc.Config{Sink: hc.NewJSONSink(&buf), SamplingRate: 1})
	op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "j"})
	hc.Add(op.Context(), "k", 1)
	op.End(nil)
	line := buf.String()
	if i := strings.Index(line, `,"duration_ms"`); i >= 0 {
		line = line[:i] + "…"
	}
	fmt.Println(line)
	// Output:
	// {"level":"info","k":1,"op.domain":"job","op.name":"j"…
}

// ExampleNewTestSink shows the in-memory sink for assertions.
func ExampleNewTestSink() {
	ts := hc.NewTestSink()
	rt := hc.MustCompile(hc.Config{Sink: ts, SamplingRate: 1})
	op := hc.Start(context.Background(), rt, hc.OperationStart{})
	hc.Add(op.Context(), "k", "v")
	op.End(nil)

	events := ts.Events()
	fmt.Println(len(events), events[0].Level(), events[0].Message())
	v, _ := events[0].Lookup("k")
	fmt.Println("k =", v)
	// Output:
	// 1 INFO operation_completed
	// k = v
}
