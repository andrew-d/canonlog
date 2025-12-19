package canonlog

import (
	"context"
	"log/slog"
	"sync"
	"testing"
)

// resetRegistry clears the global registry for testing.
func resetRegistry() {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	globalRegistry.keys = nil
}

func TestRegister(t *testing.T) {
	resetRegistry()

	attr := Register[string]("test_key")
	if attr.Key() != "test_key" {
		t.Errorf("Key() = %q, want %q", attr.Key(), "test_key")
	}
}

func TestRegister_PanicOnDuplicate(t *testing.T) {
	resetRegistry()

	Register[string]("duplicate_key")

	defer func() {
		if r := recover(); r == nil {
			t.Error("Register did not panic on duplicate key")
		}
	}()

	Register[int]("duplicate_key") // should panic
}

func TestSetAndAttrs(t *testing.T) {
	resetRegistry()

	attrUserID := Register[string]("user_id")
	attrStatus := Register[int]("status")
	attrSuccess := Register[bool]("success")

	ctx := New(context.Background())

	// Intentionally set these in a different order from registration,
	// to verify Attrs() returns in insertion order.
	Set(ctx, attrStatus, 200)
	Set(ctx, attrUserID, "usr_123")
	Set(ctx, attrSuccess, true)

	attrs := Attrs(ctx)
	if len(attrs) != 3 {
		t.Fatalf("Attrs() returned %d attributes, want 3", len(attrs))
	}

	// Check values and order
	wantAttrs := []struct {
		key   string
		value any
	}{
		{"status", int64(200)},
		{"user_id", "usr_123"},
		{"success", true},
	}

	for i, want := range wantAttrs {
		if attrs[i].Key != want.key {
			t.Errorf("attrs[%d].Key = %q, want %q", i, attrs[i].Key, want.key)
		}
		if attrs[i].Value.Any() != want.value {
			t.Errorf("attrs[%d].Value = %v, want %v", i, attrs[i].Value.Any(), want.value)
		}
	}
}

func TestOverwrite(t *testing.T) {
	resetRegistry()

	attrCount := Register[int]("count")

	ctx := New(context.Background())
	Set(ctx, attrCount, 1)
	Set(ctx, attrCount, 2)
	Set(ctx, attrCount, 3)

	attrs := Attrs(ctx)
	if len(attrs) != 1 {
		t.Fatalf("Attrs() returned %d attributes, want 1", len(attrs))
	}

	got := attrs[0].Value.Int64()
	if got != 3 {
		t.Errorf("count = %d, want 3 (last value wins)", got)
	}
}

func TestMergeFunction(t *testing.T) {
	resetRegistry()

	attrSum := Register[int]("sum", WithMerge(func(old, new int) int {
		return old + new
	}))

	ctx := New(context.Background())
	Set(ctx, attrSum, 10)
	Set(ctx, attrSum, 5)
	Set(ctx, attrSum, 3)

	attrs := Attrs(ctx)
	if len(attrs) != 1 {
		t.Fatalf("Attrs() returned %d attributes, want 1", len(attrs))
	}

	got := attrs[0].Value.Int64()
	if got != 18 {
		t.Errorf("sum = %d, want 18 (10+5+3)", got)
	}
}

func TestSetWithoutLine(t *testing.T) {
	resetRegistry()

	attr := Register[string]("orphan")

	// Set on context without Line should not panic
	ctx := context.Background()
	Set(ctx, attr, "value") // should be a no-op

	attrs := Attrs(ctx)
	if attrs != nil {
		t.Error("Attrs() on context without Line should return nil")
	}
}

func TestAttrsEmpty(t *testing.T) {
	resetRegistry()

	ctx := New(context.Background())
	attrs := Attrs(ctx)

	if attrs != nil {
		t.Errorf("Attrs() on empty Line = %v, want nil", attrs)
	}
}

func TestConcurrentSet(t *testing.T) {
	resetRegistry()

	attrCounter := Register[int]("counter", WithMerge(func(old, new int) int {
		return old + new
	}))

	ctx := New(context.Background())

	var wg sync.WaitGroup
	numGoroutines := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Set(ctx, attrCounter, 1)
		}()
	}

	wg.Wait()

	attrs := Attrs(ctx)
	if len(attrs) != 1 {
		t.Fatalf("Attrs() returned %d attributes, want 1", len(attrs))
	}

	got := attrs[0].Value.Int64()
	if got != int64(numGoroutines) {
		t.Errorf("counter = %d, want %d", got, numGoroutines)
	}
}

func TestFromContext(t *testing.T) {
	resetRegistry()

	// No line in context
	ctx := context.Background()
	if FromContext(ctx) != nil {
		t.Error("FromContext on context without Line should return nil")
	}

	// With line in context
	ctx = New(ctx)
	if FromContext(ctx) == nil {
		t.Error("FromContext on context with Line should return non-nil")
	}
}

func TestDifferentTypes(t *testing.T) {
	resetRegistry()

	attrString := Register[string]("string_val")
	attrInt := Register[int]("int_val")
	attrFloat := Register[float64]("float_val")
	attrBool := Register[bool]("bool_val")

	ctx := New(context.Background())

	Set(ctx, attrString, "hello")
	Set(ctx, attrInt, 42)
	Set(ctx, attrFloat, 3.14)
	Set(ctx, attrBool, true)

	attrs := Attrs(ctx)
	if len(attrs) != 4 {
		t.Fatalf("Attrs() returned %d attributes, want 4", len(attrs))
	}

	// Verify types are preserved
	if attrs[0].Value.String() != "hello" {
		t.Errorf("string value = %q, want %q", attrs[0].Value.String(), "hello")
	}
	if attrs[1].Value.Int64() != 42 {
		t.Errorf("int value = %d, want %d", attrs[1].Value.Int64(), 42)
	}
	if attrs[2].Value.Float64() != 3.14 {
		t.Errorf("float value = %f, want %f", attrs[2].Value.Float64(), 3.14)
	}
	if attrs[3].Value.Bool() != true {
		t.Errorf("bool value = %v, want %v", attrs[3].Value.Bool(), true)
	}
}

func TestSlogAttrCompatibility(t *testing.T) {
	resetRegistry()

	attrUser := Register[string]("user")
	ctx := New(context.Background())
	Set(ctx, attrUser, "test_user")

	attrs := Attrs(ctx)

	// Assert that Attrs returns a slice of slog.Attr.
	var _ []slog.Attr = attrs
}
