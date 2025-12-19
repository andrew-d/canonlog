// Package canonlog provides a minimal library for emitting canonical log lines.
//
// A canonical log line is a single structured log line emitted at the end of
// each request that aggregates all important information about that request.
// This pattern is useful for both human debugging and analytics.
//
// Basic usage:
//
//	// Register attributes at package level
//	var (
//		AttrUserID = canonlog.Register[string]("user_id")
//		AttrStatus = canonlog.Register[int]("status")
//	)
//
//	// In your handler
//	func handler(w http.ResponseWriter, r *http.Request) {
//		ctx := canonlog.New(r.Context())
//		canonlog.Set(ctx, AttrUserID, "usr_123")
//		canonlog.Set(ctx, AttrStatus, 200)
//
//		// At the end, emit the log line
//		slog.LogAttrs(ctx, slog.LevelInfo, "canonical-log-line", canonlog.Attrs(ctx)...)
//	}
package canonlog

import (
	"context"
	"log/slog"
	"sync"
)

// Registry tracks registered attribute keys to prevent duplicates.
// Use [NewRegistry] to create a new instance, or use [DefaultRegistry]
// for the default global registry.
type Registry struct {
	mu   sync.Mutex
	keys map[string]bool
}

// NewRegistry creates a new [Registry].
func NewRegistry() *Registry {
	return &Registry{
		keys: make(map[string]bool),
	}
}

// DefaultRegistry is the default registry used by package-level functions
// like [Register].
var DefaultRegistry = NewRegistry()

// Attr is a type-safe handle for a registered attribute.
// It is created by [Register] and used with [Set] to store values.
type Attr[T any] struct {
	key     string
	merge   func(old, new T) T
	toValue func(T) slog.Value
}

// Key returns the attribute's key name.
func (a Attr[T]) Key() string {
	return a.key
}

// Option configures an Attr during registration.
type Option[T any] func(*Attr[T])

// WithMerge sets a merge function that is called when the same attribute
// is set multiple times. The function receives the existing value and the
// new value, and returns the value to store.
//
// If no merge function is set, the default behavior is to overwrite
// the existing value with the new value.
func WithMerge[T any](fn func(old, new T) T) Option[T] {
	return func(a *Attr[T]) {
		a.merge = fn
	}
}

// WithValue sets a function to convert the attribute's value to an [slog.Value].
//
// If no conversion function is set, [slog.AnyValue] is used by default.
//
// Example:
//
//	var AttrDuration = canonlog.Register[time.Duration]("duration_sec",
//		canonlog.WithValue(func(d time.Duration) slog.Value {
//			return slog.Float64Value(d.Seconds())
//		}),
//	)
func WithValue[T any](fn func(T) slog.Value) Option[T] {
	return func(a *Attr[T]) {
		a.toValue = fn
	}
}

// RegisterWith creates a new attribute with the given key in the specified
// registry. It panics if an attribute with the same key has already been
// registered in that registry.
//
// Use [Register] for the common case of registering with [DefaultRegistry].
func RegisterWith[T any](r *Registry, key string, opts ...Option[T]) Attr[T] {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.keys == nil {
		r.keys = make(map[string]bool)
	}
	if r.keys[key] {
		panic("canonlog: duplicate attribute key: " + key)
	}
	r.keys[key] = true

	attr := Attr[T]{key: key}
	for _, opt := range opts {
		opt(&attr)
	}
	return attr
}

// Register creates a new attribute with the given key using [DefaultRegistry].
// It panics if an attribute with the same key has already been registered.
//
// Register is typically called at package initialization time:
//
//	var AttrUserID = canonlog.Register[string]("user_id")
func Register[T any](key string, opts ...Option[T]) Attr[T] {
	return RegisterWith(DefaultRegistry, key, opts...)
}

// storedValue holds a raw value and an optional converter function.
type storedValue struct {
	raw     any
	convert func(any) slog.Value
}

// Line accumulates attributes for a single canonical log line.
// It is safe for concurrent use.
type Line struct {
	mu     sync.Mutex
	values map[string]storedValue
	order  []string // maintains insertion order for consistent output
}

// ctxKey is the context key for storing the Line.
type ctxKey struct{}

// New creates a new [Line] and returns a context containing it.
//
// Use [Set] to add attributes to the line, and [Attrs] to retrieve them.
func New(ctx context.Context) context.Context {
	line := &Line{
		values: make(map[string]storedValue),
	}
	return context.WithValue(ctx, ctxKey{}, line)
}

// FromContext retrieves a [Line] from the provided [context.Context], or nil
// if none exists.
func FromContext(ctx context.Context) *Line {
	if l, ok := ctx.Value(ctxKey{}).(*Line); ok {
		return l
	}
	return nil
}

// Set stores a value for the given attribute in the [Line] attached to ctx.
// If the context does not have a Line ([New] was not called), Set silently
// does nothing.
//
// If the attribute was already set and has a merge function, the merge
// function is called to combine the old and new values. Otherwise, the
// new value overwrites the old value.
func Set[T any](ctx context.Context, attr Attr[T], value T) {
	l := FromContext(ctx)
	if l == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	key := attr.key
	if existing, exists := l.values[key]; exists && attr.merge != nil {
		if oldVal, ok := existing.raw.(T); ok {
			value = attr.merge(oldVal, value)
		}
	}

	// Track insertion order for new keys
	if _, exists := l.values[key]; !exists {
		l.order = append(l.order, key)
	}

	// Create converter function if attr has custom toValue
	var convert func(any) slog.Value
	if attr.toValue != nil {
		convert = func(v any) slog.Value { return attr.toValue(v.(T)) }
	}

	l.values[key] = storedValue{raw: value, convert: convert}
}

// Attrs returns all set attributes as [slog.Attr] values.
//
// Attributes are returned in the order they were first set. If the context
// does not have a [Line], nil is returned.
func Attrs(ctx context.Context) []slog.Attr {
	l := FromContext(ctx)
	if l == nil {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.values) == 0 {
		return nil
	}

	result := make([]slog.Attr, 0, len(l.order))
	for _, key := range l.order {
		if sv, exists := l.values[key]; exists {
			var slogVal slog.Value
			if sv.convert != nil {
				slogVal = sv.convert(sv.raw)
			} else {
				slogVal = slog.AnyValue(sv.raw)
			}
			result = append(result, slog.Attr{Key: key, Value: slogVal})
		}
	}
	return result
}
