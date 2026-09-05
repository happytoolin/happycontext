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
	st, _ := m["http.status"].(float64)
	oc, _ := m["op.outcome"].(string)
	rt, _ := m["op.name"].(string)
	usr, _ := m["wire"].(string)
	_, hasErr := m["error"]
	if st == 0 || oc == "" || rt == "" {
		t.Fatalf("%s: canonical fields missing: %v", pipeline, m)
	}
	return parsedWire{status: int64(st), outcome: oc, route: rt, user: usr, hasErr: hasErr}
}

// drive fires the wire cases at a real server built on the given real
// sink, waits for the handlers to drain, and parses the pipeline's
// emitted lines.
func drive(t *testing.T, sink gc.Sink, buf *bytes.Buffer, pipeline string) []parsedWire {
	t.Helper()
	rt := gc.MustCompile(gc.Config{Sink: sink, SamplingRate: 1})
	srv := httptest.NewServer(stdhc.Middleware(rt)(wireMux()))
	defer srv.Close()

	for _, c := range wireCases {
		resp, err := srv.Client().Get(srv.URL + c.request)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	srv.Close() // drain outstanding handlers before reading the buffer

	var out []parsedWire
	for _, ln := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if ln == "" {
			continue
		}
		out = append(out, parseWireLine(t, ln, pipeline))
	}
	return out
}

// TestAdaptersWireParity drives identical real traffic through every
// real pipeline — first-party JSONSink, slog, zap, zerolog — and
// asserts they agree on the canonical wire.
func TestAdaptersWireParity(t *testing.T) {
	pipelines := map[string][]parsedWire{}

	var jsonBuf bytes.Buffer
	pipelines["jsonsink"] = drive(t, gc.NewJSONSink(&jsonBuf), &jsonBuf, "jsonsink")

	var slogBuf bytes.Buffer
	pipelines["slog"] = drive(t, sloghc.New(slog.New(slog.NewJSONHandler(&slogBuf, nil))), &slogBuf, "slog")

	var zapBuf bytes.Buffer
	zapLogger := zap.New(zapcore.NewCore(zapcore.NewJSONEncoder(zapcore.EncoderConfig{TimeKey: "ts", LevelKey: "level", MessageKey: "msg", EncodeTime: zapcore.EpochTimeEncoder, EncodeLevel: zapcore.LowercaseLevelEncoder}), zapcore.AddSync(&zapBuf), zapcore.DebugLevel))
	pipelines["zap"] = drive(t, zaphc.New(zapLogger), &zapBuf, "zap")

	var zeroBuf bytes.Buffer
	zl := zerolog.New(&zeroBuf)
	pipelines["zerolog"] = drive(t, zerologhc.New(&zl), &zeroBuf, "zerolog")

	var reference []parsedWire
	var refName string
	for name, evs := range pipelines {
		if len(evs) != len(wireCases) {
			t.Fatalf("%s: %d events, want %d", name, len(evs), len(wireCases))
		}
		if reference == nil {
			reference, refName = evs, name
			continue
		}
		for i, ev := range evs {
			want := wireCases[i]
			if ev.status != want.status || ev.outcome != want.outcome || ev.user != want.userField {
				t.Fatalf("%s[%d] = %+v, want case %+v", name, i, ev, want)
			}
			if ev.route != reference[i].route || ev.hasErr != reference[i].hasErr {
				t.Fatalf("%s[%d] diverges from %s: %+v vs %+v", name, i, refName, ev, reference[i])
			}
		}
	}
}
