package main

import (
	"net/http"
	"os"

	"github.com/happytoolin/happycontext"
	zerologadapter "github.com/happytoolin/happycontext/adapter/zerolog"
	stdhappycontext "github.com/happytoolin/happycontext/integration/std"
	"github.com/rs/zerolog"
)

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	sink := zerologadapter.New(&logger)
	mw := stdhappycontext.Middleware(hc.Config{Sink: sink, SamplingRate: 1})

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		hc.Add(r.Context(), "example", "adapter-zerolog")
		w.WriteHeader(http.StatusOK)
	})

	_ = http.ListenAndServe(":8093", mw(mux))
}
