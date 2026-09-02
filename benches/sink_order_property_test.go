package benches_test

// P6b property (dst-research §7.4, action plan P6): order preservation
// across all sinks. One canonical record with typed fields (generated
// key-value pairs; keys unique, so the expected order is the record's
// insertion order) runs through the first-party JSON sink and the
// slog/zap/zerolog bridges; each output line is parsed and the user
// keys must appear in the SAME relative order in every sink, with
// exact value equality for the kinds every adapter renders faithfully.
//
// This lives in benches because it imports the adapters, which are
// separate modules with replace directives.
//
// Documented divergences kept OUT of the value-equality set (their
// keys still participate in the ORDER assertion):
//   - time: each sink stamps its own rendering (epoch millis on zap,
//     RFC3339Nano on slog, RFC3339 on hc/zerolog)
//   - duration: float milliseconds on the hc JSON path vs the native
//     encodings of the host loggers
//   - float32: widened to float64 by slog (documented host limitation)
//   - raw: base64 on slog/zap; any: adapter-specific rendering

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"testing"
	"time"

	hc "github.com/happytoolin/happycontext"
	slogadapter "github.com/happytoolin/happycontext/adapter/slog"
	zapadapter "github.com/happytoolin/happycontext/adapter/zap"
	zerologadapter "github.com/happytoolin/happycontext/adapter/zerolog"
	"github.com/rs/zerolog"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// orderErr is a plain error type (message-only rendering everywhere).
type orderErr string

func (e orderErr) Error() string { return string(e) }

// orderPair is one generated (key, value) addition.
type orderPair struct {
	key   string
	value any
}

func genOrderPairs(rng *rand.Rand) ([]orderPair, map[string]any) {
	n := 8 + rng.IntN(12)
	pairs := make([]orderPair, 0, n)
	expected := map[string]any{}
	for i := 0; i < n; i++ {
		k := fmt.Sprintf("f%02d", i)
		switch i % 7 {
		case 0:
			v := fmt.Sprintf("str-%d-☃", rng.IntN(1e6))
			pairs = append(pairs, orderPair{k, v})
			expected[k] = v
		case 1:
			v := int64(rng.IntN(1<<30)) * 1000
			pairs = append(pairs, orderPair{k, v})
			expected[k] = json.Number(strconv.FormatInt(v, 10))
		case 2:
			v := int64(rng.IntN(1 << 20))
			pairs = append(pairs, orderPair{k, uint64(v)})
			expected[k] = json.Number(strconv.FormatUint(uint64(v), 10))
		case 3:
			v := rng.Float64()*2 - 1
			pairs = append(pairs, orderPair{k, v})
			expected[k] = json.Number(strconv.FormatFloat(v, 'g', -1, 64))
		case 4:
			v := rng.Uint64()&1 == 0
			pairs = append(pairs, orderPair{k, v})
			expected[k] = v
		case 5:
			v := fmt.Sprintf("err-%d", rng.IntN(100))
			pairs = append(pairs, orderPair{k, orderErr(v)})
			expected[k] = v
		default:
			// time: zero nanos in a fixed zone — present in every line,
			// exact-value equality excluded (adapter formats differ)
			tm := time.Date(2026, 9, 1, 10+rng.IntN(5), 0, 0, 0, time.FixedZone("+05:30", 5*3600+1800))
			pairs = append(pairs, orderPair{k, tm})
		}
	}
	return pairs, expected
}

// sinkBuilder wires one sink to a buffer.
type sinkBuilder func(*bytes.Buffer) hc.Sink

func sinkBuilders() map[string]sinkBuilder {
	return map[string]sinkBuilder{
		"hc-json": func(buf *bytes.Buffer) hc.Sink { return hc.NewJSONSink(buf) },
		"slog": func(buf *bytes.Buffer) hc.Sink {
			return slogadapter.New(slog.New(slog.NewJSONHandler(buf, nil)))
		},
		"zap": func(buf *bytes.Buffer) hc.Sink {
			core := zapcore.NewCore(
				zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
				zapcore.AddSync(buf), zapcore.DebugLevel,
			)
			return zapadapter.New(zap.New(core))
		},
		"zerolog": func(buf *bytes.Buffer) hc.Sink {
			zl := zerolog.New(buf)
			return zerologadapter.New(&zl)
		},
	}
}

// parseOrdered parses one sink line into ordered keys and a value map
// (json.Number preserved).
func parseOrdered(t *testing.T, line []byte) ([]string, map[string]any) {
	t.Helper()
	if len(line) == 0 {
		t.Fatal("empty sink output")
	}
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("not valid JSON: %v (%q)", err, line)
	}
	var keys []string
	dec2 := json.NewDecoder(bytes.NewReader(line))
	dec2.UseNumber()
	tok, err := dec2.Token()
	if err != nil || tok != json.Delim('{') {
		t.Fatalf("not an object: %v (%q)", err, line)
	}
	for {
		tok, err := dec2.Token()
		if err != nil {
			t.Fatalf("member walk: %v", err)
		}
		if tok == json.Delim('}') {
			break
		}
		key, _ := tok.(string)
		keys = append(keys, key)
		var raw json.RawMessage
		if err := dec2.Decode(&raw); err != nil {
			t.Fatalf("member %q: %v", key, err)
		}
	}
	return keys, m
}

// orderValueEqual compares parsed values; numbers compare numerically
// (each sink may format them differently).
func orderValueEqual(got, want any) bool {
	switch w := want.(type) {
	case json.Number:
		g, ok := got.(json.Number)
		if !ok {
			return false
		}
		gf, err1 := g.Float64()
		wf, err2 := w.Float64()
		return err1 == nil && err2 == nil && gf == wf
	case string:
		g, ok := got.(string)
		return ok && g == w
	case bool:
		g, ok := got.(bool)
		return ok && g == w
	default:
		return false
	}
}

// TestSinkOrderPreservationProperty drives generated records through
// the four sinks and asserts identical user-key order plus faithful
// value equality.
func TestSinkOrderPreservationProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(0x0AD0A5eed, 0x8A7E5EED))
	builders := sinkBuilders()
	for i := 0; i < 200; i++ {
		pairs, expected := genOrderPairs(rng)
		userKeys := make([]string, 0, len(pairs))
		for _, p := range pairs {
			userKeys = append(userKeys, p.key)
		}

		for name, build := range builders {
			var buf bytes.Buffer
			rt := hc.MustCompile(hc.Config{Sink: build(&buf), SamplingRate: 1})
			op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "order"})
			ctx := op.Context()
			for _, p := range pairs {
				hc.Add(ctx, p.key, p.value)
			}
			op.End(nil)
			line := bytes.TrimSpace(buf.Bytes())

			keys, vals := parseOrdered(t, line)
			// The user keys must appear as an exact ordered subsequence:
			// each adapter may interleave its own envelope keys (level,
			// ts, msg, time, ...) but never reorder the fields.
			sub := 0
			for _, k := range keys {
				if sub < len(userKeys) && k == userKeys[sub] {
					sub++
				}
			}
			if sub != len(userKeys) {
				t.Fatalf("iter %d: %s emitted user keys out of order\n got %v\nwant subsequence %v",
					i, name, keys, userKeys)
			}
			// Faithful kinds compare exactly across all four sinks.
			for k, want := range expected {
				got, ok := vals[k]
				if !ok {
					t.Fatalf("iter %d: %s missing key %q (line %s)", i, name, k, line)
				}
				if !orderValueEqual(got, want) {
					t.Fatalf("iter %d: %s key %q = %v (%T), want %v", i, name, k, got, got, want)
				}
			}
		}
	}
}
