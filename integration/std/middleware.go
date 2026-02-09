package stdhappycontext

import (
	"io"
	"net/http"

	"github.com/felixge/httpsnoop"
	"github.com/happytoolin/happycontext"
	"github.com/happytoolin/happycontext/integration/common"
)

// Config controls standard net/http middleware behavior.
type Config = happycontext.Config

// Middleware wraps an http.Handler with happycontext request lifecycle logging.
func Middleware(cfg Config) func(http.Handler) http.Handler {
	cfg = common.NormalizeConfig(cfg)
	sink := cfg.Sink

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if sink == nil {
				next.ServeHTTP(w, r)
				return
			}

			ctx, event := common.StartRequest(r.Context(), r.Method, r.URL.Path)

			req := r.WithContext(ctx)
			tracker := &responseWriter{}
			ww := httpsnoop.Wrap(w, httpsnoop.Hooks{
				WriteHeader: tracker.writeHeaderHook,
				Write:       tracker.writeHook,
				ReadFrom:    tracker.readFromHook,
			})

			defer func() {
				recovered := recover()
				status := common.ResolveStatus(tracker.statusCode, nil, recovered, tracker.wroteHeader, 0)
				common.FinalizeRequest(cfg, common.FinalizeInput{
					Ctx:        ctx,
					Event:      event,
					Method:     req.Method,
					Path:       req.URL.Path,
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

type responseWriter struct {
	statusCode  int
	wroteHeader bool
}

func (rw *responseWriter) writeHeaderHook(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
	return func(code int) {
		if !rw.wroteHeader {
			rw.statusCode = code
			rw.wroteHeader = true
		}
		next(code)
	}
}

func (rw *responseWriter) writeHook(next httpsnoop.WriteFunc) httpsnoop.WriteFunc {
	return func(p []byte) (int, error) {
		if !rw.wroteHeader {
			rw.statusCode = http.StatusOK
			rw.wroteHeader = true
		}
		return next(p)
	}
}

func (rw *responseWriter) readFromHook(next httpsnoop.ReadFromFunc) httpsnoop.ReadFromFunc {
	return func(src io.Reader) (int64, error) {
		if !rw.wroteHeader {
			rw.statusCode = http.StatusOK
			rw.wroteHeader = true
		}
		return next(src)
	}
}
