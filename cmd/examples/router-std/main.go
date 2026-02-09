package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/happytoolin/hlog"
	slogadapter "github.com/happytoolin/hlog/adapter/slog"
	stdhlog "github.com/happytoolin/hlog/integration/std"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := slogadapter.New(logger)
	mw := stdhlog.Middleware(hlog.Config{Sink: sink, SamplingRate: 1})

	mux := http.NewServeMux()
	mux.HandleFunc("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		hlog.Add(r.Context(), "router", "net/http")
		w.WriteHeader(http.StatusOK)
	})

	_ = http.ListenAndServe(":8101", mw(mux))
}
