package main

import (
	"net/http"

	"github.com/happytoolin/happycontext"
	zapadapter "github.com/happytoolin/happycontext/adapter/zap"
	stdhappycontext "github.com/happytoolin/happycontext/integration/std"
	"go.uber.org/zap"
)

func main() {
	logger := zap.NewExample()
	sink := zapadapter.New(logger)
	mw := stdhappycontext.Middleware(happycontext.Config{Sink: sink, SamplingRate: 1})

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		happycontext.Add(r.Context(), "example", "adapter-zap")
		w.WriteHeader(http.StatusOK)
	})

	_ = http.ListenAndServe(":8092", mw(mux))
}
