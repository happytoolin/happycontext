package hc

import "context"

type contextKey struct{}

// FromContext returns the request event stored in ctx, or nil if absent.
func FromContext(ctx context.Context) *Event {
	if ctx == nil {
		return nil
	}
	e, _ := ctx.Value(contextKey{}).(*Event)
	return e
}

// NewContext attaches a new event to ctx and returns both.
func NewContext(ctx context.Context) (context.Context, *Event) {
	e := NewEvent()
	return context.WithValue(ctx, contextKey{}, e), e
}

// Add records one field on the event stored in ctx.
func Add(ctx context.Context, key string, value any) bool {
	e := FromContext(ctx)
	if e == nil {
		return false
	}
	e.Add(key, value)
	return true
}

// AddMap merges all fields into the event stored in ctx.
func AddMap(ctx context.Context, fields map[string]any) bool {
	e := FromContext(ctx)
	if e == nil {
		return false
	}
	e.AddMap(fields)
	return true
}

// Error records err on the event stored in ctx.
func Error(ctx context.Context, err error) bool {
	e := FromContext(ctx)
	if e == nil {
		return false
	}
	e.SetError(err)
	return true
}

// SetLevel sets a requested level override for the event in ctx.
func SetLevel(ctx context.Context, level Level) bool {
	e := FromContext(ctx)
	if e == nil {
		return false
	}
	return e.SetLevel(level)
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
		return e.RequestedLevel()
	}
	return Level(""), false
}
