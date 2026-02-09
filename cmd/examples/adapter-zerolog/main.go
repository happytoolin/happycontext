package main

import (
	"net/http"
	"os"

	"github.com/happytoolin/hlog"
	zerologadapter "github.com/happytoolin/hlog/adapter/zerolog"
	stdhlog "github.com/happytoolin/hlog/integration/std"
	"github.com/rs/zerolog"
)

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	sink := zerologadapter.New(&logger)
	mw := stdhlog.Middleware(hlog.Config{Sink: sink, SamplingRate: 1})

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		hlog.Add(r.Context(), "example", "adapter-zerolog")
		w.WriteHeader(http.StatusOK)
	})

	_ = http.ListenAndServe(":8093", mw(mux))
}
