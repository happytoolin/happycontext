package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/happytoolin/happycontext"
	slogadapter "github.com/happytoolin/happycontext/adapter/slog"
	stdhappycontext "github.com/happytoolin/happycontext/integration/std"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := slogadapter.New(logger)
	mw := stdhappycontext.Middleware(hc.Config{Sink: sink, SamplingRate: 1})

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		hc.Add(r.Context(), "example", "adapter-slog")
		w.WriteHeader(http.StatusOK)
	})

	_ = http.ListenAndServe(":8091", mw(mux))
}
