package hc

import (
	"context"
	"sync"
	"time"
)

type contextKey struct{}

type pooledEventContext struct {
	parent context.Context
	event  *Event
}

var pooledEventContextPool = sync.Pool{
	New: func() any {
		return &pooledEventContext{}
	},
}

func (ctx *pooledEventContext) Deadline() (time.Time, bool) {
	return ctx.parent.Deadline()
}

func (ctx *pooledEventContext) Done() <-chan struct{} {
	return ctx.parent.Done()
}

func (ctx *pooledEventContext) Err() error {
	return ctx.parent.Err()
}

func (ctx *pooledEventContext) Value(key any) any {
	if _, ok := key.(contextKey); ok {
		return ctx.event
	}
	return ctx.parent.Value(key)
}

// FromContext returns the request event stored in ctx, or nil if absent.
func FromContext(ctx context.Context) *Event {
	if ctx == nil {
		return nil
	}
	if pctx, ok := ctx.(*pooledEventContext); ok {
		return pctx.event
	}
	e, _ := ctx.Value(contextKey{}).(*Event)
	return e
}

// NewContext attaches a new event to ctx and returns both.
func NewContext(ctx context.Context) (context.Context, *Event) {
	if ctx == nil {
		ctx = context.Background()
	}
	e := newEvent()
	return context.WithValue(ctx, contextKey{}, e), e
}

// NewPooledContext attaches a pooled event to ctx.
//
// The returned context must be released with ReleasePooledContext after the
// request or operation is finalized. This is intended for integrations that own
// the full request lifecycle.
func NewPooledContext(ctx context.Context) (context.Context, *Event) {
	if ctx == nil {
		ctx = context.Background()
	}
	e := newPooledEvent()
	pctx := pooledEventContextPool.Get().(*pooledEventContext)
	pctx.parent = ctx
	pctx.event = e
	return pctx, e
}

// ReleasePooledContext returns a context and event obtained from
// NewPooledContext to their pools. Callers must not use ctx or the returned
// event after releasing it.
func ReleasePooledContext(ctx context.Context) {
	pctx, ok := ctx.(*pooledEventContext)
	if !ok {
		return
	}
	releaseEvent(pctx.event)
	pctx.parent = nil
	pctx.event = nil
	pooledEventContextPool.Put(pctx)
}

// Add records one or more fields on the event stored in ctx.
//
// Additional key/value pairs can be passed via kv:
// Add(ctx, "a", 1, "b", 2, "c", 3).
// kv must have even length and every key position must be a string.
func Add(ctx context.Context, key string, value any, kv ...any) bool {
	e := FromContext(ctx)
	if e == nil {
		return false
	}
	if len(kv) == 0 {
		e.mu.Lock()
		e.setFieldLocked(key, value)
		e.mu.Unlock()
		return true
	}
	return e.addKV(key, value, kv...)
}

// Error records err on the event stored in ctx.
func Error(ctx context.Context, err error) bool {
	e := FromContext(ctx)
	if e == nil {
		return false
	}
	e.setError(err)
	return true
}

// SetMessage records a per-event message on the event stored in ctx.
// An empty message is treated as unset during finalization.
func SetMessage(ctx context.Context, msg string) bool {
	e := FromContext(ctx)
	if e == nil {
		return false
	}
	e.setMessage(msg)
	return true
}

// SetLevel sets a requested level override for the event in ctx.
func SetLevel(ctx context.Context, level Level) bool {
	e := FromContext(ctx)
	if e == nil {
		return false
	}
	return e.setLevel(level)
}

// SetRoute sets a normalized route template on the event in ctx.
func SetRoute(ctx context.Context, route string) bool {
	e := FromContext(ctx)
	if e == nil {
		return false
	}
	e.setRoute(route)
	return true
}

// GetLevel returns a previously requested level override from ctx.
func GetLevel(ctx context.Context) (Level, bool) {
	if e := FromContext(ctx); e != nil {
		return e.requestedLevelValue()
	}
	return Level(""), false
}
