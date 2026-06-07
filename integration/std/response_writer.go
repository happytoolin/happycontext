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

type closeNotifier interface {
	CloseNotify() <-chan bool
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

	responseWriterCPool     sync.Pool
	responseWriterFCPool    sync.Pool
	responseWriterHCPool    sync.Pool
	responseWriterPCPool    sync.Pool
	responseWriterRCPool    sync.Pool
	responseWriterFHCPool   sync.Pool
	responseWriterFPCPool   sync.Pool
	responseWriterFRCPool   sync.Pool
	responseWriterHPCPool   sync.Pool
	responseWriterHRCPool   sync.Pool
	responseWriterPRCPool   sync.Pool
	responseWriterFHPCPool  sync.Pool
	responseWriterFHRCPool  sync.Pool
	responseWriterFPRCPool  sync.Pool
	responseWriterHPRCPool  sync.Pool
	responseWriterFHPRCPool sync.Pool
)

func wrapResponseWriter(w http.ResponseWriter) trackedResponseWriter {
	hasFlush := hasFlusher(w)
	hasHijack := hasHijacker(w)
	hasPush := hasPusher(w)
	hasReadFrom := hasReaderFrom(w)
	hasCloseNotify := hasCloseNotifier(w)

	if hasCloseNotify {
		switch {
		case hasFlush && hasHijack && hasPush && hasReadFrom:
			return getResponseWriterFHPRC(w)
		case hasFlush && hasHijack && hasPush:
			return getResponseWriterFHPC(w)
		case hasFlush && hasHijack && hasReadFrom:
			return getResponseWriterFHRC(w)
		case hasFlush && hasPush && hasReadFrom:
			return getResponseWriterFPRC(w)
		case hasHijack && hasPush && hasReadFrom:
			return getResponseWriterHPRC(w)
		case hasFlush && hasHijack:
			return getResponseWriterFHC(w)
		case hasFlush && hasPush:
			return getResponseWriterFPC(w)
		case hasFlush && hasReadFrom:
			return getResponseWriterFRC(w)
		case hasHijack && hasPush:
			return getResponseWriterHPC(w)
		case hasHijack && hasReadFrom:
			return getResponseWriterHRC(w)
		case hasPush && hasReadFrom:
			return getResponseWriterPRC(w)
		case hasFlush:
			return getResponseWriterFC(w)
		case hasHijack:
			return getResponseWriterHC(w)
		case hasPush:
			return getResponseWriterPC(w)
		case hasReadFrom:
			return getResponseWriterRC(w)
		default:
			return getResponseWriterC(w)
		}
	}

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
		typed.reset(nil)
		responseWriterFPool.Put(typed)
	case *responseWriterH:
		typed.reset(nil)
		responseWriterHPool.Put(typed)
	case *responseWriterP:
		typed.reset(nil)
		responseWriterPPool.Put(typed)
	case *responseWriterR:
		typed.reset(nil)
		responseWriterRPool.Put(typed)
	case *responseWriterFH:
		typed.reset(nil)
		responseWriterFHPool.Put(typed)
	case *responseWriterFP:
		typed.reset(nil)
		responseWriterFPPool.Put(typed)
	case *responseWriterFR:
		typed.reset(nil)
		responseWriterFRPool.Put(typed)
	case *responseWriterHP:
		typed.reset(nil)
		responseWriterHPPool.Put(typed)
	case *responseWriterHR:
		typed.reset(nil)
		responseWriterHRPool.Put(typed)
	case *responseWriterPR:
		typed.reset(nil)
		responseWriterPRPool.Put(typed)
	case *responseWriterFHP:
		typed.reset(nil)
		responseWriterFHPPool.Put(typed)
	case *responseWriterFHR:
		typed.reset(nil)
		responseWriterFHRPool.Put(typed)
	case *responseWriterFPR:
		typed.reset(nil)
		responseWriterFPRPool.Put(typed)
	case *responseWriterHPR:
		typed.reset(nil)
		responseWriterHPRPool.Put(typed)
	case *responseWriterFHPR:
		typed.reset(nil)
		responseWriterFHPRPool.Put(typed)
	case *responseWriterC:
		typed.reset(nil)
		responseWriterCPool.Put(typed)
	case *responseWriterFC:
		typed.reset(nil)
		responseWriterFCPool.Put(typed)
	case *responseWriterHC:
		typed.reset(nil)
		responseWriterHCPool.Put(typed)
	case *responseWriterPC:
		typed.reset(nil)
		responseWriterPCPool.Put(typed)
	case *responseWriterRC:
		typed.reset(nil)
		responseWriterRCPool.Put(typed)
	case *responseWriterFHC:
		typed.reset(nil)
		responseWriterFHCPool.Put(typed)
	case *responseWriterFPC:
		typed.reset(nil)
		responseWriterFPCPool.Put(typed)
	case *responseWriterFRC:
		typed.reset(nil)
		responseWriterFRCPool.Put(typed)
	case *responseWriterHPC:
		typed.reset(nil)
		responseWriterHPCPool.Put(typed)
	case *responseWriterHRC:
		typed.reset(nil)
		responseWriterHRCPool.Put(typed)
	case *responseWriterPRC:
		typed.reset(nil)
		responseWriterPRCPool.Put(typed)
	case *responseWriterFHPC:
		typed.reset(nil)
		responseWriterFHPCPool.Put(typed)
	case *responseWriterFHRC:
		typed.reset(nil)
		responseWriterFHRCPool.Put(typed)
	case *responseWriterFPRC:
		typed.reset(nil)
		responseWriterFPRCPool.Put(typed)
	case *responseWriterHPRC:
		typed.reset(nil)
		responseWriterHPRCPool.Put(typed)
	case *responseWriterFHPRC:
		typed.reset(nil)
		responseWriterFHPRCPool.Put(typed)
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
	rw.reset(w)
	return rw
}

func getResponseWriterH(w http.ResponseWriter) *responseWriterH {
	rw, _ := responseWriterHPool.Get().(*responseWriterH)
	if rw == nil {
		rw = &responseWriterH{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterP(w http.ResponseWriter) *responseWriterP {
	rw, _ := responseWriterPPool.Get().(*responseWriterP)
	if rw == nil {
		rw = &responseWriterP{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterR(w http.ResponseWriter) *responseWriterR {
	rw, _ := responseWriterRPool.Get().(*responseWriterR)
	if rw == nil {
		rw = &responseWriterR{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterFH(w http.ResponseWriter) *responseWriterFH {
	rw, _ := responseWriterFHPool.Get().(*responseWriterFH)
	if rw == nil {
		rw = &responseWriterFH{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterFP(w http.ResponseWriter) *responseWriterFP {
	rw, _ := responseWriterFPPool.Get().(*responseWriterFP)
	if rw == nil {
		rw = &responseWriterFP{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterFR(w http.ResponseWriter) *responseWriterFR {
	rw, _ := responseWriterFRPool.Get().(*responseWriterFR)
	if rw == nil {
		rw = &responseWriterFR{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterHP(w http.ResponseWriter) *responseWriterHP {
	rw, _ := responseWriterHPPool.Get().(*responseWriterHP)
	if rw == nil {
		rw = &responseWriterHP{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterHR(w http.ResponseWriter) *responseWriterHR {
	rw, _ := responseWriterHRPool.Get().(*responseWriterHR)
	if rw == nil {
		rw = &responseWriterHR{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterPR(w http.ResponseWriter) *responseWriterPR {
	rw, _ := responseWriterPRPool.Get().(*responseWriterPR)
	if rw == nil {
		rw = &responseWriterPR{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterFHP(w http.ResponseWriter) *responseWriterFHP {
	rw, _ := responseWriterFHPPool.Get().(*responseWriterFHP)
	if rw == nil {
		rw = &responseWriterFHP{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterFHR(w http.ResponseWriter) *responseWriterFHR {
	rw, _ := responseWriterFHRPool.Get().(*responseWriterFHR)
	if rw == nil {
		rw = &responseWriterFHR{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterFPR(w http.ResponseWriter) *responseWriterFPR {
	rw, _ := responseWriterFPRPool.Get().(*responseWriterFPR)
	if rw == nil {
		rw = &responseWriterFPR{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterHPR(w http.ResponseWriter) *responseWriterHPR {
	rw, _ := responseWriterHPRPool.Get().(*responseWriterHPR)
	if rw == nil {
		rw = &responseWriterHPR{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterFHPR(w http.ResponseWriter) *responseWriterFHPR {
	rw, _ := responseWriterFHPRPool.Get().(*responseWriterFHPR)
	if rw == nil {
		rw = &responseWriterFHPR{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterC(w http.ResponseWriter) *responseWriterC {
	rw, _ := responseWriterCPool.Get().(*responseWriterC)
	if rw == nil {
		rw = &responseWriterC{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterFC(w http.ResponseWriter) *responseWriterFC {
	rw, _ := responseWriterFCPool.Get().(*responseWriterFC)
	if rw == nil {
		rw = &responseWriterFC{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterHC(w http.ResponseWriter) *responseWriterHC {
	rw, _ := responseWriterHCPool.Get().(*responseWriterHC)
	if rw == nil {
		rw = &responseWriterHC{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterPC(w http.ResponseWriter) *responseWriterPC {
	rw, _ := responseWriterPCPool.Get().(*responseWriterPC)
	if rw == nil {
		rw = &responseWriterPC{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterRC(w http.ResponseWriter) *responseWriterRC {
	rw, _ := responseWriterRCPool.Get().(*responseWriterRC)
	if rw == nil {
		rw = &responseWriterRC{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterFHC(w http.ResponseWriter) *responseWriterFHC {
	rw, _ := responseWriterFHCPool.Get().(*responseWriterFHC)
	if rw == nil {
		rw = &responseWriterFHC{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterFPC(w http.ResponseWriter) *responseWriterFPC {
	rw, _ := responseWriterFPCPool.Get().(*responseWriterFPC)
	if rw == nil {
		rw = &responseWriterFPC{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterFRC(w http.ResponseWriter) *responseWriterFRC {
	rw, _ := responseWriterFRCPool.Get().(*responseWriterFRC)
	if rw == nil {
		rw = &responseWriterFRC{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterHPC(w http.ResponseWriter) *responseWriterHPC {
	rw, _ := responseWriterHPCPool.Get().(*responseWriterHPC)
	if rw == nil {
		rw = &responseWriterHPC{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterHRC(w http.ResponseWriter) *responseWriterHRC {
	rw, _ := responseWriterHRCPool.Get().(*responseWriterHRC)
	if rw == nil {
		rw = &responseWriterHRC{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterPRC(w http.ResponseWriter) *responseWriterPRC {
	rw, _ := responseWriterPRCPool.Get().(*responseWriterPRC)
	if rw == nil {
		rw = &responseWriterPRC{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterFHPC(w http.ResponseWriter) *responseWriterFHPC {
	rw, _ := responseWriterFHPCPool.Get().(*responseWriterFHPC)
	if rw == nil {
		rw = &responseWriterFHPC{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterFHRC(w http.ResponseWriter) *responseWriterFHRC {
	rw, _ := responseWriterFHRCPool.Get().(*responseWriterFHRC)
	if rw == nil {
		rw = &responseWriterFHRC{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterFPRC(w http.ResponseWriter) *responseWriterFPRC {
	rw, _ := responseWriterFPRCPool.Get().(*responseWriterFPRC)
	if rw == nil {
		rw = &responseWriterFPRC{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterHPRC(w http.ResponseWriter) *responseWriterHPRC {
	rw, _ := responseWriterHPRCPool.Get().(*responseWriterHPRC)
	if rw == nil {
		rw = &responseWriterHPRC{}
	}
	rw.reset(w)
	return rw
}

func getResponseWriterFHPRC(w http.ResponseWriter) *responseWriterFHPRC {
	rw, _ := responseWriterFHPRCPool.Get().(*responseWriterFHPRC)
	if rw == nil {
		rw = &responseWriterFHPRC{}
	}
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
