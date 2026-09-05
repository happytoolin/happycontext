package examples

// Cross-bridge wire reality: the std middleware driven by the same
// real traffic through each REAL logger bridge (slog, zap, zerolog)
// plus the first-party JSONSink. No test doubles anywhere: the oracles
// parse each pipeline's native JSON output and assert the bridges
// agree on the canonical fields for identical requests. (The envelope
// member names differ per host — msg/message — but the canonical and
// user fields are shared, which is exactly the parity that matters.)

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gc "github.com/happytoolin/happycontext"
	sloghc "github.com/happytoolin/happycontext/adapter/slog"
	zaphc "github.com/happytoolin/happycontext/adapter/zap"
	zerologhc "github.com/happytoolin/happycontext/adapter/zerolog"
	stdhc "github.com/happytoolin/happycontext/integration/std"
	"github.com/rs/zerolog"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var wireCases = []struct {
	request   string
	status    int64
	outcome   string
	userField string
}{
	{"/ok/a1", 200, "success", "ok-field"},
	{"/teapot/a2", 418, "success", "teapot-field"},
	{"/fail/a3", 500, "failure", "fail-field"},
}

func wireMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ok/{id}", func(w http.ResponseWriter, r *http.Request) {
		gc.Add(r.Context(), "wire", "ok-field")
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /teapot/{id}", func(w http.ResponseWriter, r *http.Request) {
		gc.Add(r.Context(), "wire", "teapot-field")
		w.WriteHeader(http.StatusTeapot)
	})
	mux.HandleFunc("GET /fail/{id}", func(w http.ResponseWriter, r *http.Request) {
		gc.Add(r.Context(), "wire", "fail-field")
		gc.Error(r.Context(), fmt.Errorf("bridge failure"))
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	return mux
}

type parsedWire struct {
	status  int64
	outcome string
	route   string
	user    string
	hasErr  bool
}

func parseWireLine(t *testing.T, ln, pipeline string) parsedWire {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(ln), &m); err != nil {
		t.Fatalf("%s line unparseable: %v: %s", pipeline, err, ln)
	}
	st, stOK := m["http.status"].(float64)
	oc, ocOK := m["op.outcome"].(string)
	rt, rtOK := m["op.name"].(string)
	usr, _ := m["wire"].(string)
	_, hasErr := m["error"]
	if !stOK || !ocOK || !rtOK {
		t.Fatalf("%s: canonical fields missing or mistyped: %v", pipeline, m)
	}
	return parsedWire{status: int64(st), outcome: oc, route: rt, user: usr, hasErr: hasErr}
}

// drivePipeline fires the wire cases at a real server built on the
// given real sink and parses the pipeline's emitted lines. The
// explicit Close is load-bearing: client returns precede the server's
// deferred emissions, and Close waits for outstanding handlers.
func drivePipeline(t *testing.T, sink gc.Sink, buf *bytes.Buffer, pipeline string) []parsedWire {
	t.Helper()
	rt := gc.MustCompile(gc.Config{Sink: sink, SamplingRate: 1})
	srv := httptest.NewServer(stdhc.Middleware(rt)(wireMux()))
	defer srv.Close() // safety net for early fatals; Close is idempotent

	for _, c := range wireCases {
		resp, err := srv.Client().Get(srv.URL + c.request)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	srv.Close() // quiesce handlers before reading the buffer

	out := make([]parsedWire, 0, len(wireCases))
	for ln := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if ln == "" {
			continue
		}
		out = append(out, parseWireLine(t, ln, pipeline))
	}
	return out
}

// TestAdaptersWireParity drives identical real traffic through every
// real pipeline — first-party JSONSink, slog, zap, zerolog — and
// asserts BOTH the absolute canonical fields per pipeline AND that the
// bridges agree with the JSONSink reference. Deterministic order
// (jsonsink is the fixed reference), no map-iteration roulette.
func TestAdaptersWireParity(t *testing.T) {
	var jsonBuf bytes.Buffer
	reference := drivePipeline(t, gc.NewJSONSink(&jsonBuf), &jsonBuf, "jsonsink")

	var slogBuf bytes.Buffer
	slogEvents := drivePipeline(t, sloghc.New(slog.New(slog.NewJSONHandler(&slogBuf, nil))), &slogBuf, "slog")

	var zapBuf bytes.Buffer
	zapLogger := zap.New(zapcore.NewCore(zapcore.NewJSONEncoder(zapcore.EncoderConfig{TimeKey: "ts", LevelKey: "level", MessageKey: "msg", EncodeTime: zapcore.EpochTimeEncoder, EncodeLevel: zapcore.LowercaseLevelEncoder}), zapcore.Lock(zapcore.AddSync(&zapBuf)), zapcore.DebugLevel))
	zapEvents := drivePipeline(t, zaphc.New(zapLogger), &zapBuf, "zap")

	var zeroBuf bytes.Buffer
	zl := zerolog.New(&zeroBuf)
	zeroEvents := drivePipeline(t, zerologhc.New(&zl), &zeroBuf, "zerolog")

	pipelines := []struct {
		name   string
		events []parsedWire
	}{
		{"jsonsink", reference},
		{"slog", slogEvents},
		{"zap", zapEvents},
		{"zerolog", zeroEvents},
	}
	for _, pl := range pipelines {
		if len(pl.events) != len(wireCases) {
			t.Fatalf("%s: %d events, want %d", pl.name, len(pl.events), len(wireCases))
		}
		for i, ev := range pl.events {
			// Absolute checks for EVERY pipeline — a bridge regressing a
			// canonical field must fail deterministically, not only when
			// map iteration happens to make it the non-reference.
			want := wireCases[i]
			if ev.status != want.status || ev.outcome != want.outcome || ev.user != want.userField {
				t.Fatalf("%s[%d] = %+v, want case %+v", pl.name, i, ev, want)
			}
			if ev.route != reference[i].route || ev.hasErr != reference[i].hasErr {
				t.Fatalf("%s[%d] diverges from jsonsink: %+v vs %+v", pl.name, i, ev, reference[i])
			}
		}
	}
}
