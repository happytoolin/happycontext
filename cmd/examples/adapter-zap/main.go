package main

import (
	"errors"
	"net/http"

	"github.com/happytoolin/happycontext"
	zapadapter "github.com/happytoolin/happycontext/adapter/zap"
	stdhappycontext "github.com/happytoolin/happycontext/integration/std"
	"go.uber.org/zap"
)

func main() {
	logger := zap.NewExample()
	sink := zapadapter.New(logger)
	mw := stdhappycontext.Middleware(hc.Config{Sink: sink, SamplingRate: 1})

	mux := http.NewServeMux()
	mux.HandleFunc("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := r.PathValue("id")

		hc.Add(ctx, "example", "adapter-zap")
		hc.Add(ctx, "event_attached", hc.FromContext(ctx) != nil)
		hc.AddMap(ctx, map[string]any{
			"user": map[string]any{
				"id":   id,
				"plan": "pro",
			},
			"request": map[string]any{
				"feature": "checkout",
				"tags":    []string{"examples", "zap"},
			},
		})
		hc.SetRoute(ctx, "/users/{id}")

		if r.URL.Query().Get("debug") == "1" {
			hc.SetLevel(ctx, hc.LevelDebug)
		}
		if level, ok := hc.GetLevel(ctx); ok {
			hc.Add(ctx, "requested_level", level)
		}
		if r.URL.Query().Get("fail") == "1" {
			hc.Error(ctx, errors.New("demo failure"))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	_ = http.ListenAndServe(":8102", mw(mux))
}
