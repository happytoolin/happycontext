package stdhappycontext

import (
	"net/http"

	"github.com/happytoolin/happycontext"
	"github.com/happytoolin/happycontext/integration/common"
)

// Config controls standard net/http middleware behavior.
type Config = hc.Config

// Middleware wraps an http.Handler with happycontext request lifecycle logging.
func Middleware(cfg Config) func(http.Handler) http.Handler {
	prepared := common.PrepareRequestConfig(cfg)
	sink := prepared.Config.Sink

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if sink == nil {
				next.ServeHTTP(w, r)
				return
			}

			ctx, event := common.StartRequest(r.Context(), r.Method, r.URL.Path)
			req := r
			oldCtx, swappedCtx := common.SwapRequestContextUnsafe(r, ctx)
			if !swappedCtx {
				req = r.WithContext(ctx)
			}
			ww := wrapResponseWriter(w)

			defer func() {
				recovered := recover()
				statusCode, wroteHeader := ww.status()
				status := common.ResolveStatus(statusCode, nil, recovered, wroteHeader, 0)
				if swappedCtx {
					_, _ = common.SwapRequestContextUnsafe(r, oldCtx)
				}
				common.FinalizePreparedRequest(prepared, common.FinalizeInput{
					Ctx:        ctx,
					Event:      event,
					Route:      req.Pattern,
					StatusCode: status,
					Recovered:  recovered,
				})

				if recovered != nil {
					panic(recovered)
				}
			}()

			next.ServeHTTP(ww, req)
		})
	}
}
