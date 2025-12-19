package canonlog

import (
	"context"
	"log/slog"
	"sync"
	"testing"
)

// testRegistry returns a new registry for use in a single test.
func testRegistry(tb testing.TB) *Registry {
	reg := NewRegistry()
	tb.Cleanup(func() {
		if !tb.Failed() {
			return
		}

		reg.mu.Lock()
		defer reg.mu.Unlock()
		for key := range reg.keys {
			tb.Logf("registered key: %q", key)
		}
	})
	return reg
}

func TestRegister(t *testing.T) {
	r := testRegistry(t)

	attr := RegisterWith[string](r, "test_key")
	if attr.Key() != "test_key" {
		t.Errorf("Key() = %q, want %q", attr.Key(), "test_key")
	}
}

func TestRegister_PanicOnDuplicate(t *testing.T) {
	r := testRegistry(t)

	RegisterWith[string](r, "duplicate_key")

	defer func() {
		if r := recover(); r == nil {
			t.Error("Register did not panic on duplicate key")
		}
	}()

	RegisterWith[int](r, "duplicate_key") // should panic
}

func TestSetAndAttrs(t *testing.T) {
	r := testRegistry(t)

	attrUserID := RegisterWith[string](r, "user_id")
	attrStatus := RegisterWith[int](r, "status")
	attrSuccess := RegisterWith[bool](r, "success")

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
	r := testRegistry(t)

	attrCount := RegisterWith[int](r, "count")

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
	r := testRegistry(t)

	attrSum := RegisterWith[int](r, "sum", WithMerge(func(old, new int) int {
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
	r := testRegistry(t)

	attr := RegisterWith[string](r, "orphan")

	// Set on context without Line should not panic
	ctx := context.Background()
	Set(ctx, attr, "value") // should be a no-op

	attrs := Attrs(ctx)
	if attrs != nil {
		t.Error("Attrs() on context without Line should return nil")
	}
}

func TestAttrsEmpty(t *testing.T) {
	ctx := New(context.Background())
	attrs := Attrs(ctx)

	if attrs != nil {
		t.Errorf("Attrs() on empty Line = %v, want nil", attrs)
	}
}

func TestConcurrentSet(t *testing.T) {
	r := testRegistry(t)

	attrCounter := RegisterWith[int](r, "counter", WithMerge(func(old, new int) int {
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
	r := testRegistry(t)

	attrString := RegisterWith[string](r, "string_val")
	attrInt := RegisterWith[int](r, "int_val")
	attrFloat := RegisterWith[float64](r, "float_val")
	attrBool := RegisterWith[bool](r, "bool_val")

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
	r := testRegistry(t)

	attrUser := RegisterWith[string](r, "user")
	ctx := New(context.Background())
	Set(ctx, attrUser, "test_user")

	attrs := Attrs(ctx)

	// Assert that Attrs returns a slice of slog.Attr.
	var _ []slog.Attr = attrs
}
