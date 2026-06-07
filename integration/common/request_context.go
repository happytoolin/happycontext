package common

import (
	"context"
	"net/http"
	"reflect"
	"unsafe"
)

var (
	requestContextOffset uintptr
	requestContextOK     bool
)

func init() {
	field, ok := reflect.TypeOf(http.Request{}).FieldByName("ctx")
	if !ok {
		return
	}
	if field.Type != reflect.TypeOf((*context.Context)(nil)).Elem() {
		return
	}
	requestContextOffset = field.Offset
	requestContextOK = true
}

// SwapRequestContextUnsafe replaces r's context in place and returns the
// previous context. Callers must restore the returned context before releasing
// any pooled context assigned to r.
func SwapRequestContextUnsafe(r *http.Request, ctx context.Context) (context.Context, bool) {
	if r == nil || !requestContextOK {
		return nil, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	old := r.Context()
	*(*context.Context)(unsafe.Add(unsafe.Pointer(r), requestContextOffset)) = ctx
	return old, true
}
