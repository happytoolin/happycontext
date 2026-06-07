package stdhappycontext

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"sync"
)

type trackedResponseWriter interface {
	http.ResponseWriter
	status() (int, bool)
}

type responseWriter struct {
	w           http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

var (
	responseWriterPool     sync.Pool
	responseWriterFPool    sync.Pool
	responseWriterHPool    sync.Pool
	responseWriterPPool    sync.Pool
	responseWriterRPool    sync.Pool
	responseWriterFHPool   sync.Pool
	responseWriterFPPool   sync.Pool
	responseWriterFRPool   sync.Pool
	responseWriterHPPool   sync.Pool
	responseWriterHRPool   sync.Pool
	responseWriterPRPool   sync.Pool
	responseWriterFHPPool  sync.Pool
	responseWriterFHRPool  sync.Pool
	responseWriterFPRPool  sync.Pool
	responseWriterHPRPool  sync.Pool
	responseWriterFHPRPool sync.Pool
)

func wrapResponseWriter(w http.ResponseWriter) trackedResponseWriter {
	hasFlush := hasFlusher(w)
	hasHijack := hasHijacker(w)
	hasPush := hasPusher(w)
	hasReadFrom := hasReaderFrom(w)

	switch {
	case hasFlush && hasHijack && hasPush && hasReadFrom:
		return getResponseWriterFHPR(w)
	case hasFlush && hasHijack && hasPush:
		return getResponseWriterFHP(w)
	case hasFlush && hasHijack && hasReadFrom:
		return getResponseWriterFHR(w)
	case hasFlush && hasPush && hasReadFrom:
		return getResponseWriterFPR(w)
	case hasHijack && hasPush && hasReadFrom:
		return getResponseWriterHPR(w)
	case hasFlush && hasHijack:
		return getResponseWriterFH(w)
	case hasFlush && hasPush:
		return getResponseWriterFP(w)
	case hasFlush && hasReadFrom:
		return getResponseWriterFR(w)
	case hasHijack && hasPush:
		return getResponseWriterHP(w)
	case hasHijack && hasReadFrom:
		return getResponseWriterHR(w)
	case hasPush && hasReadFrom:
		return getResponseWriterPR(w)
	case hasFlush:
		return getResponseWriterF(w)
	case hasHijack:
		return getResponseWriterH(w)
	case hasPush:
		return getResponseWriterP(w)
	case hasReadFrom:
		return getResponseWriterR(w)
	default:
		return getResponseWriter(w)
	}
}

func releaseResponseWriter(rw trackedResponseWriter) {
	switch typed := rw.(type) {
	case *responseWriter:
		typed.reset(nil)
		responseWriterPool.Put(typed)
	case *responseWriterF:
		typed.responseWriter.reset(nil)
		responseWriterFPool.Put(typed)
	case *responseWriterH:
		typed.responseWriter.reset(nil)
		responseWriterHPool.Put(typed)
	case *responseWriterP:
		typed.responseWriter.reset(nil)
		responseWriterPPool.Put(typed)
	case *responseWriterR:
		typed.responseWriter.reset(nil)
		responseWriterRPool.Put(typed)
	case *responseWriterFH:
		typed.responseWriter.reset(nil)
		responseWriterFHPool.Put(typed)
	case *responseWriterFP:
		typed.responseWriter.reset(nil)
		responseWriterFPPool.Put(typed)
	case *responseWriterFR:
		typed.responseWriter.reset(nil)
		responseWriterFRPool.Put(typed)
	case *responseWriterHP:
		typed.responseWriter.reset(nil)
		responseWriterHPPool.Put(typed)
	case *responseWriterHR:
		typed.responseWriter.reset(nil)
		responseWriterHRPool.Put(typed)
	case *responseWriterPR:
		typed.responseWriter.reset(nil)
		responseWriterPRPool.Put(typed)
	case *responseWriterFHP:
		typed.responseWriter.reset(nil)
		responseWriterFHPPool.Put(typed)
	case *responseWriterFHR:
		typed.responseWriter.reset(nil)
		responseWriterFHRPool.Put(typed)
	case *responseWriterFPR:
		typed.responseWriter.reset(nil)
		responseWriterFPRPool.Put(typed)
	case *responseWriterHPR:
		typed.responseWriter.reset(nil)
		responseWriterHPRPool.Put(typed)
	case *responseWriterFHPR:
		typed.responseWriter.reset(nil)
		responseWriterFHPRPool.Put(typed)
	}
}

func getResponseWriter(w http.ResponseWriter) *responseWriter {
	rw, _ := responseWriterPool.Get().(*responseWriter)
	if rw == nil {
		rw = &responseWriter{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterF(w http.ResponseWriter) *responseWriterF {
	rw, _ := responseWriterFPool.Get().(*responseWriterF)
	if rw == nil {
		rw = &responseWriterF{}
	}
	rw.responseWriter.reset(w)
	return rw
}

func getResponseWriterH(w http.ResponseWriter) *responseWriterH {
	rw, _ := responseWriterHPool.Get().(*responseWriterH)
	if rw == nil {
		rw = &responseWriterH{}
	}
	rw.responseWriter.reset(w)
	return rw
}

func getResponseWriterP(w http.ResponseWriter) *responseWriterP {
	rw, _ := responseWriterPPool.Get().(*responseWriterP)
	if rw == nil {
		rw = &responseWriterP{}
	}
	rw.responseWriter.reset(w)
	return rw
}

func getResponseWriterR(w http.ResponseWriter) *responseWriterR {
	rw, _ := responseWriterRPool.Get().(*responseWriterR)
	if rw == nil {
		rw = &responseWriterR{}
	}
	rw.responseWriter.reset(w)
	return rw
}

func getResponseWriterFH(w http.ResponseWriter) *responseWriterFH {
	rw, _ := responseWriterFHPool.Get().(*responseWriterFH)
	if rw == nil {
		rw = &responseWriterFH{}
	}
	rw.responseWriter.reset(w)
	return rw
}

func getResponseWriterFP(w http.ResponseWriter) *responseWriterFP {
	rw, _ := responseWriterFPPool.Get().(*responseWriterFP)
	if rw == nil {
		rw = &responseWriterFP{}
	}
	rw.responseWriter.reset(w)
	return rw
}

func getResponseWriterFR(w http.ResponseWriter) *responseWriterFR {
	rw, _ := responseWriterFRPool.Get().(*responseWriterFR)
	if rw == nil {
		rw = &responseWriterFR{}
	}
	rw.responseWriter.reset(w)
	return rw
}

func getResponseWriterHP(w http.ResponseWriter) *responseWriterHP {
	rw, _ := responseWriterHPPool.Get().(*responseWriterHP)
	if rw == nil {
		rw = &responseWriterHP{}
	}
	rw.responseWriter.reset(w)
	return rw
}

func getResponseWriterHR(w http.ResponseWriter) *responseWriterHR {
	rw, _ := responseWriterHRPool.Get().(*responseWriterHR)
	if rw == nil {
		rw = &responseWriterHR{}
	}
	rw.responseWriter.reset(w)
	return rw
}

func getResponseWriterPR(w http.ResponseWriter) *responseWriterPR {
	rw, _ := responseWriterPRPool.Get().(*responseWriterPR)
	if rw == nil {
		rw = &responseWriterPR{}
	}
	rw.responseWriter.reset(w)
	return rw
}

func getResponseWriterFHP(w http.ResponseWriter) *responseWriterFHP {
	rw, _ := responseWriterFHPPool.Get().(*responseWriterFHP)
	if rw == nil {
		rw = &responseWriterFHP{}
	}
	rw.responseWriter.reset(w)
	return rw
}

func getResponseWriterFHR(w http.ResponseWriter) *responseWriterFHR {
	rw, _ := responseWriterFHRPool.Get().(*responseWriterFHR)
	if rw == nil {
		rw = &responseWriterFHR{}
	}
	rw.responseWriter.reset(w)
	return rw
}

func getResponseWriterFPR(w http.ResponseWriter) *responseWriterFPR {
	rw, _ := responseWriterFPRPool.Get().(*responseWriterFPR)
	if rw == nil {
		rw = &responseWriterFPR{}
	}
	rw.responseWriter.reset(w)
	return rw
}

func getResponseWriterHPR(w http.ResponseWriter) *responseWriterHPR {
	rw, _ := responseWriterHPRPool.Get().(*responseWriterHPR)
	if rw == nil {
		rw = &responseWriterHPR{}
	}
	rw.responseWriter.reset(w)
	return rw
}

func getResponseWriterFHPR(w http.ResponseWriter) *responseWriterFHPR {
	rw, _ := responseWriterFHPRPool.Get().(*responseWriterFHPR)
	if rw == nil {
		rw = &responseWriterFHPR{}
	}
	rw.responseWriter.reset(w)
	return rw
}

func hasFlusher(w http.ResponseWriter) bool {
	_, ok := w.(http.Flusher)
	return ok
}

func hasHijacker(w http.ResponseWriter) bool {
	_, ok := w.(http.Hijacker)
	return ok
}

func hasPusher(w http.ResponseWriter) bool {
	_, ok := w.(http.Pusher)
	return ok
}

func hasReaderFrom(w http.ResponseWriter) bool {
	_, ok := w.(io.ReaderFrom)
	return ok
}

func (rw *responseWriter) Header() http.Header {
	return rw.w.Header()
}

func (rw *responseWriter) Write(p []byte) (int, error) {
	rw.markStatus(http.StatusOK)
	return rw.w.Write(p)
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.markStatus(code)
	rw.w.WriteHeader(code)
}

func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.w
}

func (rw *responseWriter) status() (int, bool) {
	return rw.statusCode, rw.wroteHeader
}

func (rw *responseWriter) markStatus(code int) {
	if rw.wroteHeader {
		return
	}
	rw.statusCode = code
	rw.wroteHeader = true
}

func (rw *responseWriter) reset(w http.ResponseWriter) {
	rw.w = w
	rw.statusCode = 0
	rw.wroteHeader = false
}

func (rw *responseWriter) flush() {
	rw.markStatus(http.StatusOK)
	rw.w.(http.Flusher).Flush()
}

func (rw *responseWriter) hijack() (net.Conn, *bufio.ReadWriter, error) {
	return rw.w.(http.Hijacker).Hijack()
}

func (rw *responseWriter) push(target string, opts *http.PushOptions) error {
	return rw.w.(http.Pusher).Push(target, opts)
}

func (rw *responseWriter) readFrom(src io.Reader) (int64, error) {
	rw.markStatus(http.StatusOK)
	return rw.w.(io.ReaderFrom).ReadFrom(src)
}

type responseWriterF struct{ responseWriter }

func (rw *responseWriterF) Flush() { rw.flush() }

type responseWriterH struct{ responseWriter }

func (rw *responseWriterH) Hijack() (net.Conn, *bufio.ReadWriter, error) { return rw.hijack() }

type responseWriterP struct{ responseWriter }

func (rw *responseWriterP) Push(target string, opts *http.PushOptions) error {
	return rw.push(target, opts)
}

type responseWriterR struct{ responseWriter }

func (rw *responseWriterR) ReadFrom(src io.Reader) (int64, error) { return rw.readFrom(src) }

type responseWriterFH struct{ responseWriter }

func (rw *responseWriterFH) Flush() { rw.flush() }
func (rw *responseWriterFH) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return rw.hijack()
}

type responseWriterFP struct{ responseWriter }

func (rw *responseWriterFP) Flush() { rw.flush() }
func (rw *responseWriterFP) Push(target string, opts *http.PushOptions) error {
	return rw.push(target, opts)
}

type responseWriterFR struct{ responseWriter }

func (rw *responseWriterFR) Flush()                                { rw.flush() }
func (rw *responseWriterFR) ReadFrom(src io.Reader) (int64, error) { return rw.readFrom(src) }

type responseWriterHP struct{ responseWriter }

func (rw *responseWriterHP) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return rw.hijack()
}
func (rw *responseWriterHP) Push(target string, opts *http.PushOptions) error {
	return rw.push(target, opts)
}

type responseWriterHR struct{ responseWriter }

func (rw *responseWriterHR) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return rw.hijack()
}
func (rw *responseWriterHR) ReadFrom(src io.Reader) (int64, error) { return rw.readFrom(src) }

type responseWriterPR struct{ responseWriter }

func (rw *responseWriterPR) Push(target string, opts *http.PushOptions) error {
	return rw.push(target, opts)
}
func (rw *responseWriterPR) ReadFrom(src io.Reader) (int64, error) { return rw.readFrom(src) }

type responseWriterFHP struct{ responseWriter }

func (rw *responseWriterFHP) Flush() { rw.flush() }
func (rw *responseWriterFHP) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return rw.hijack()
}
func (rw *responseWriterFHP) Push(target string, opts *http.PushOptions) error {
	return rw.push(target, opts)
}

type responseWriterFHR struct{ responseWriter }

func (rw *responseWriterFHR) Flush() { rw.flush() }
func (rw *responseWriterFHR) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return rw.hijack()
}
func (rw *responseWriterFHR) ReadFrom(src io.Reader) (int64, error) { return rw.readFrom(src) }

type responseWriterFPR struct{ responseWriter }

func (rw *responseWriterFPR) Flush() { rw.flush() }
func (rw *responseWriterFPR) Push(target string, opts *http.PushOptions) error {
	return rw.push(target, opts)
}
func (rw *responseWriterFPR) ReadFrom(src io.Reader) (int64, error) { return rw.readFrom(src) }

type responseWriterHPR struct{ responseWriter }

func (rw *responseWriterHPR) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return rw.hijack()
}
func (rw *responseWriterHPR) Push(target string, opts *http.PushOptions) error {
	return rw.push(target, opts)
}
func (rw *responseWriterHPR) ReadFrom(src io.Reader) (int64, error) { return rw.readFrom(src) }

type responseWriterFHPR struct{ responseWriter }

func (rw *responseWriterFHPR) Flush() { rw.flush() }
func (rw *responseWriterFHPR) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return rw.hijack()
}
func (rw *responseWriterFHPR) Push(target string, opts *http.PushOptions) error {
	return rw.push(target, opts)
}
func (rw *responseWriterFHPR) ReadFrom(src io.Reader) (int64, error) {
	return rw.readFrom(src)
}
