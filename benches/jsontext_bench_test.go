//go:build go1.27

package benches_test

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"io"
	"strconv"
	"testing"
	"time"

	hc "github.com/happytoolin/happycontext"
)

// jsontextEncode builds the same canonical line the first-party encoder
// produces, but with Go 1.27's encoding/json/jsontext appends — the
// maintenance-free comparator backend kept in benches/ per amendment 10,
// so the fork can be re-raced against the stdlib per Go release.
func jsontextEncode(rec *hc.Record) []byte {
	dst := make([]byte, 0, 512)
	dst = append(dst, `{"level":"`...)
	dst = append(dst, jsonLevelText(rec.Level())...)
	dst = append(dst, '"')

	fields := rec.Fields()
	if len(fields) <= 24 { // mirror the encoder's allocation-free narrow path
		for i := range fields {
			f := fields[i]
			last := true
			for j := i + 1; j < len(fields); j++ {
				if fields[j].Key() == f.Key() {
					last = false
					break
				}
			}
			if last {
				dst = append(dst, ',')
				dst, _ = jsontext.AppendQuote(dst, f.Key())
				dst = append(dst, ':')
				dst = appendJSONTextField(dst, f)
			}
		}
	} else {
		seen := map[string]struct{}{}
		kept := make([]int, 0, 16)
		for i := len(fields) - 1; i >= 0; i-- {
			if _, dup := seen[fields[i].Key()]; dup {
				continue
			}
			seen[fields[i].Key()] = struct{}{}
			kept = append(kept, i)
		}
		for i := len(kept) - 1; i >= 0; i-- {
			f := fields[kept[i]]
			dst = append(dst, ',')
			dst, _ = jsontext.AppendQuote(dst, f.Key())
			dst = append(dst, ':')
			dst = appendJSONTextField(dst, f)
		}
	}

	dst = append(dst, ',', '"')
	dst, _ = jsontext.AppendQuote(dst, "time")
	dst = append(dst, ':', '"')
	dst = time.Now().AppendFormat(dst, time.RFC3339)
	dst = append(dst, '"', ',', '"')
	dst, _ = jsontext.AppendQuote(dst, "message")
	dst = append(dst, ':')
	dst, _ = jsontext.AppendQuote(dst, rec.Message())
	dst = append(dst, '}', '\n')
	return dst
}

// appendJSONTextField covers the canonical field shapes (strings, ints,
// bools, durations) — the bench corpus subset, not the full type set.
func appendJSONTextField(dst []byte, f hc.Field) []byte {
	if s, ok := f.Str(); ok {
		dst, _ = jsontext.AppendQuote(dst, s)
		return dst
	}
	if i, ok := f.Int(); ok {
		return strconv.AppendInt(dst, i, 10)
	}
	if b, ok := f.Bool(); ok {
		return strconv.AppendBool(dst, b)
	}
	if d, ok := f.Duration(); ok {
		return jsontext.AppendFloat(dst, float64(d)/float64(time.Millisecond), 64)
	}
	blob, err := json.Marshal(f.Any())
	if err != nil {
		dst, _ = jsontext.AppendQuote(dst, err.Error())
		return dst
	}
	return append(dst, blob...)
}

func jsonLevelText(level hc.Level) string {
	switch level {
	case hc.LevelDebug:
		return "debug"
	case hc.LevelWarn:
		return "warn"
	case hc.LevelError:
		return "error"
	default:
		return "info"
	}
}

type jsontextSink struct{ w io.Writer }

func (s *jsontextSink) Write(_ context.Context, rec *hc.Record) {
	_, _ = s.w.Write(jsontextEncode(rec))
}

// BenchmarkJSONSinkJsontextComparator races the jsontext backend against
// the first-party encoder on the same event shape.
var rtFork = hc.MustCompile(hc.Config{Sink: hc.NewJSONSink(io.Discard), SamplingRate: 1})

func BenchmarkJSONSinkJsontextComparator(b *testing.B) {
	cap := &recordCapture{}
	rt := hc.MustCompile(hc.Config{Sink: cap, SamplingRate: 1})
	for range 64 {
		op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainHTTP, Name: "GET /api/v1/orders/:id"})
		benchmarkFields(op.Context(), 12)
		op.End(nil)
	}
	recs := cap.recs

	js := &jsontextSink{w: io.Discard}
	fork := hc.NewJSONSink(io.Discard)

	b.Run("jsontext_12_fields", func(b *testing.B) {
		b.ReportAllocs()
		i := 0
		for b.Loop() {
			js.Write(context.Background(), recs[i&63])
			i++
		}
	})
	b.Run("fork_12_fields_preencoded", func(b *testing.B) {
		b.ReportAllocs()
		i := 0
		for b.Loop() {
			fork.Write(context.Background(), recs[i&63])
			i++
		}
	})
	b.Run("fork_12_fields_full_encode", func(b *testing.B) {
		// fresh records each iteration: measures encode-once + write
		b.ReportAllocs()
		for b.Loop() {
			op := hc.Start(context.Background(), rtFork, hc.OperationStart{Domain: hc.DomainHTTP, Name: "GET /api/v1/orders/:id"})
			benchmarkFields(op.Context(), 12)
			op.End(nil)
		}
	})
}
