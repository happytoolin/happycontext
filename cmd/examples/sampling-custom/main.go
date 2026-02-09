package main

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/happytoolin/happycontext"
	slogadapter "github.com/happytoolin/happycontext/adapter/slog"
	stdhappycontext "github.com/happytoolin/happycontext/integration/std"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := slogadapter.New(logger)

	mw := stdhappycontext.Middleware(hc.Config{
		Sink: sink,
		Sampler: func(in hc.SampleInput) bool {
			if in.HasError || in.StatusCode >= 500 {
				return true
			}
			if in.Duration >= 500*time.Millisecond {
				return true
			}
			fields := hc.EventFields(in.Event)
			tier, _ := fields["user_tier"].(string)
			return tier == "enterprise"
		},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := r.PathValue("id")
		tier := r.URL.Query().Get("tier")
		if tier == "" {
			tier = "free"
		}

		hc.Add(ctx, "router", "sampling-custom")
		hc.Add(ctx, "user_id", id)
		hc.Add(ctx, "user_tier", tier)
		hc.SetRoute(ctx, r.Pattern)

		if r.URL.Query().Get("slow") == "1" {
			time.Sleep(650 * time.Millisecond)
		}
		if r.URL.Query().Get("fail") == "1" {
			hc.Error(ctx, errors.New("demo failure"))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	_ = http.ListenAndServe(":8110", mw(mux))
}
