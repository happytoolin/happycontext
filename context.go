package hlog

import "context"

type contextKey struct{}

// FromContext returns the request event stored in ctx, or nil if absent.
func FromContext(ctx context.Context) *Event {
	e, _ := ctx.Value(contextKey{}).(*Event)
	return e
}

// NewContext attaches a new event to ctx and returns both.
func NewContext(ctx context.Context) (context.Context, *Event) {
	e := NewEvent()
	return context.WithValue(ctx, contextKey{}, e), e
}

// Add records one field on the event stored in ctx.
func Add(ctx context.Context, key string, value any) {
	if e := FromContext(ctx); e != nil {
		e.Add(key, value)
	}
}

// Set is an alias of Add.
func Set(ctx context.Context, key string, value any) {
	Add(ctx, key, value)
}

// AddMap merges all fields into the event stored in ctx.
func AddMap(ctx context.Context, fields map[string]any) {
	if e := FromContext(ctx); e != nil {
		e.AddMap(fields)
	}
}

// Error records err on the event stored in ctx.
func Error(ctx context.Context, err error) {
	if e := FromContext(ctx); e != nil {
		e.SetError(err)
	}
}

// SetError is an alias of Error.
func SetError(ctx context.Context, err error) {
	Error(ctx, err)
}

// SetLevel sets a requested level override for the event in ctx.
func SetLevel(ctx context.Context, level string) {
	if e := FromContext(ctx); e != nil {
		e.SetLevel(level)
	}
}

// SetRoute sets a normalized route template on the event in ctx.
func SetRoute(ctx context.Context, route string) {
	if e := FromContext(ctx); e != nil {
		e.setRoute(route)
	}
}

// GetLevel returns a previously requested level override from ctx.
func GetLevel(ctx context.Context) (string, bool) {
	if e := FromContext(ctx); e != nil {
		return e.RequestedLevel()
	}
	return "", false
}
