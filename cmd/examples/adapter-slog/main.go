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
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		hlog.Add(r.Context(), "example", "adapter-slog")
		w.WriteHeader(http.StatusOK)
	})

	_ = http.ListenAndServe(":8091", mw(mux))
}
