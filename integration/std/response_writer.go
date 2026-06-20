package stdhappycontext

import (
	"bufio"
	"io"
	"net"
	"net/http"
)

type trackedResponseWriter interface {
	http.ResponseWriter
	status() (int, bool)
}

type closeNotifier interface {
	CloseNotify() <-chan bool
}

type responseWriter struct {
	w           http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func wrapResponseWriter(w http.ResponseWriter) trackedResponseWriter {
	hasFlush := hasFlusher(w)
	hasHijack := hasHijacker(w)
	hasPush := hasPusher(w)
	hasReadFrom := hasReaderFrom(w)
	hasCloseNotify := hasCloseNotifier(w)

	if hasCloseNotify {
		switch {
		case hasFlush && hasHijack && hasPush && hasReadFrom:
			return initResponseWriter(w, &responseWriterFHPRC{})
		case hasFlush && hasHijack && hasPush:
			return initResponseWriter(w, &responseWriterFHPC{})
		case hasFlush && hasHijack && hasReadFrom:
			return initResponseWriter(w, &responseWriterFHRC{})
		case hasFlush && hasPush && hasReadFrom:
			return initResponseWriter(w, &responseWriterFPRC{})
		case hasHijack && hasPush && hasReadFrom:
			return initResponseWriter(w, &responseWriterHPRC{})
		case hasFlush && hasHijack:
			return initResponseWriter(w, &responseWriterFHC{})
		case hasFlush && hasPush:
			return initResponseWriter(w, &responseWriterFPC{})
		case hasFlush && hasReadFrom:
			return initResponseWriter(w, &responseWriterFRC{})
		case hasHijack && hasPush:
			return initResponseWriter(w, &responseWriterHPC{})
		case hasHijack && hasReadFrom:
			return initResponseWriter(w, &responseWriterHRC{})
		case hasPush && hasReadFrom:
			return initResponseWriter(w, &responseWriterPRC{})
		case hasFlush:
			return initResponseWriter(w, &responseWriterFC{})
		case hasHijack:
			return initResponseWriter(w, &responseWriterHC{})
		case hasPush:
			return initResponseWriter(w, &responseWriterPC{})
		case hasReadFrom:
			return initResponseWriter(w, &responseWriterRC{})
		default:
			return initResponseWriter(w, &responseWriterC{})
		}
	}

	switch {
	case hasFlush && hasHijack && hasPush && hasReadFrom:
		return initResponseWriter(w, &responseWriterFHPR{})
	case hasFlush && hasHijack && hasPush:
		return initResponseWriter(w, &responseWriterFHP{})
	case hasFlush && hasHijack && hasReadFrom:
		return initResponseWriter(w, &responseWriterFHR{})
	case hasFlush && hasPush && hasReadFrom:
		return initResponseWriter(w, &responseWriterFPR{})
	case hasHijack && hasPush && hasReadFrom:
		return initResponseWriter(w, &responseWriterHPR{})
	case hasFlush && hasHijack:
		return initResponseWriter(w, &responseWriterFH{})
	case hasFlush && hasPush:
		return initResponseWriter(w, &responseWriterFP{})
	case hasFlush && hasReadFrom:
		return initResponseWriter(w, &responseWriterFR{})
	case hasHijack && hasPush:
		return initResponseWriter(w, &responseWriterHP{})
	case hasHijack && hasReadFrom:
		return initResponseWriter(w, &responseWriterHR{})
	case hasPush && hasReadFrom:
		return initResponseWriter(w, &responseWriterPR{})
	case hasFlush:
		return initResponseWriter(w, &responseWriterF{})
	case hasHijack:
		return initResponseWriter(w, &responseWriterH{})
	case hasPush:
		return initResponseWriter(w, &responseWriterP{})
	case hasReadFrom:
		return initResponseWriter(w, &responseWriterR{})
	default:
		return initResponseWriter(w, &responseWriter{})
	}
}

func initResponseWriter[T interface {
	reset(http.ResponseWriter)
}](w http.ResponseWriter, rw T) T {
	rw.reset(w)
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

func hasCloseNotifier(w http.ResponseWriter) bool {
	_, ok := w.(closeNotifier)
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

func (rw *responseWriter) closeNotify() <-chan bool {
	return rw.w.(closeNotifier).CloseNotify()
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

type responseWriterC struct{ responseWriter }

func (rw *responseWriterC) CloseNotify() <-chan bool { return rw.closeNotify() }

type responseWriterFC struct{ responseWriterF }

func (rw *responseWriterFC) CloseNotify() <-chan bool { return rw.closeNotify() }

type responseWriterHC struct{ responseWriterH }

func (rw *responseWriterHC) CloseNotify() <-chan bool { return rw.closeNotify() }

type responseWriterPC struct{ responseWriterP }

func (rw *responseWriterPC) CloseNotify() <-chan bool { return rw.closeNotify() }

type responseWriterRC struct{ responseWriterR }

func (rw *responseWriterRC) CloseNotify() <-chan bool { return rw.closeNotify() }

type responseWriterFHC struct{ responseWriterFH }

func (rw *responseWriterFHC) CloseNotify() <-chan bool { return rw.closeNotify() }

type responseWriterFPC struct{ responseWriterFP }

func (rw *responseWriterFPC) CloseNotify() <-chan bool { return rw.closeNotify() }

type responseWriterFRC struct{ responseWriterFR }

func (rw *responseWriterFRC) CloseNotify() <-chan bool { return rw.closeNotify() }

type responseWriterHPC struct{ responseWriterHP }

func (rw *responseWriterHPC) CloseNotify() <-chan bool { return rw.closeNotify() }

type responseWriterHRC struct{ responseWriterHR }

func (rw *responseWriterHRC) CloseNotify() <-chan bool { return rw.closeNotify() }

type responseWriterPRC struct{ responseWriterPR }

func (rw *responseWriterPRC) CloseNotify() <-chan bool { return rw.closeNotify() }

type responseWriterFHPC struct{ responseWriterFHP }

func (rw *responseWriterFHPC) CloseNotify() <-chan bool { return rw.closeNotify() }

type responseWriterFHRC struct{ responseWriterFHR }

func (rw *responseWriterFHRC) CloseNotify() <-chan bool { return rw.closeNotify() }

type responseWriterFPRC struct{ responseWriterFPR }

func (rw *responseWriterFPRC) CloseNotify() <-chan bool { return rw.closeNotify() }

type responseWriterHPRC struct{ responseWriterHPR }

func (rw *responseWriterHPRC) CloseNotify() <-chan bool { return rw.closeNotify() }

type responseWriterFHPRC struct{ responseWriterFHPR }

func (rw *responseWriterFHPRC) CloseNotify() <-chan bool { return rw.closeNotify() }
