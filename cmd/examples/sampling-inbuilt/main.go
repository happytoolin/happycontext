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
		Sampler: hc.ChainSampler(
			hc.RateSampler(0.05),
			hc.KeepErrors(),
			hc.KeepPathPrefix("/users/vip"),
			hc.KeepSlowerThan(250*time.Millisecond),
		),
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleUser(w, r, "standard")
	})
	mux.HandleFunc("/users/vip/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleUser(w, r, "vip")
	})

	_ = http.ListenAndServe(":8109", mw(mux))
}

func handleUser(w http.ResponseWriter, r *http.Request, tier string) {
	ctx := r.Context()
	id := r.PathValue("id")

	hc.Add(ctx, "router", "sampling-inbuilt")
	hc.Add(ctx, "user_id", id)
	hc.Add(ctx, "user_tier", tier)
	hc.SetRoute(ctx, r.Pattern)

	if r.URL.Query().Get("slow") == "1" {
		time.Sleep(350 * time.Millisecond)
	}
	if r.URL.Query().Get("fail") == "1" {
		hc.Error(ctx, errors.New("demo failure"))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
