package main

import (
	"net/http"

	"github.com/happytoolin/hlog"
	zapadapter "github.com/happytoolin/hlog/adapter/zap"
	stdhlog "github.com/happytoolin/hlog/integration/std"
	"go.uber.org/zap"
)

func main() {
	logger := zap.NewExample()
	sink := zapadapter.New(logger)
	mw := stdhlog.Middleware(hlog.Config{Sink: sink, SamplingRate: 1})

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		hlog.Add(r.Context(), "example", "adapter-zap")
		w.WriteHeader(http.StatusOK)
	})

	_ = http.ListenAndServe(":8092", mw(mux))
}
