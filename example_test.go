package canonlog_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/andrew-d/canonlog"
)

var (
	AttrUserID     = canonlog.Register[string]("user_id")
	AttrHTTPMethod = canonlog.Register[string]("http_method")
	AttrHTTPPath   = canonlog.Register[string]("http_path")
	AttrHTTPStatus = canonlog.Register[int]("http_status")
	AttrDuration   = canonlog.Register[time.Duration]("duration")
)

func Example_basic() {
	// Create a new context with a canonical log line
	ctx := canonlog.New(context.Background())

	// Set attributes throughout your request lifecycle
	canonlog.Set(ctx, AttrUserID, "usr_123")
	canonlog.Set(ctx, AttrHTTPMethod, "POST")
	canonlog.Set(ctx, AttrHTTPPath, "/v1/charges")
	canonlog.Set(ctx, AttrHTTPStatus, 200)
	canonlog.Set(ctx, AttrDuration, 150*time.Millisecond)

	// Get all attributes for logging
	attrs := canonlog.Attrs(ctx)

	// Print for demonstration (normally you'd use slog.LogAttrs)
	for _, attr := range attrs {
		fmt.Printf("%s=%v\n", attr.Key, attr.Value)
	}

	// Output:
	// user_id=usr_123
	// http_method=POST
	// http_path=/v1/charges
	// http_status=200
	// duration=150ms
}

// AttrErrorCount demonstrates using a merge function to accumulate values.
var AttrErrorCount = canonlog.Register[int]("error_count",
	canonlog.WithMerge(func(old, new int) int { return old + new }))

func Example_withMerge() {
	ctx := canonlog.New(context.Background())

	// Multiple errors occur during request processing
	canonlog.Set(ctx, AttrErrorCount, 1) // First error
	canonlog.Set(ctx, AttrErrorCount, 1) // Second error
	canonlog.Set(ctx, AttrErrorCount, 1) // Third error

	attrs := canonlog.Attrs(ctx)
	fmt.Printf("%s=%v\n", attrs[0].Key, attrs[0].Value)

	// Output:
	// error_count=3
}

// TestHTTPMiddleware demonstrates a typical HTTP middleware pattern with
// canonical log lines.
func TestHTTPMiddleware(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logOutput bytes.Buffer

		// Create middleware that logs to our buffer
		middleware := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				start := time.Now()
				ctx := canonlog.New(r.Context())

				canonlog.Set(ctx, AttrHTTPMethod, r.Method)
				canonlog.Set(ctx, AttrHTTPPath, r.URL.Path)

				wrapped := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

				defer func() {
					canonlog.Set(ctx, AttrHTTPStatus, wrapped.status)
					canonlog.Set(ctx, AttrDuration, time.Since(start))

					// Log to buffer with deterministic output (no timestamp)
					logger := slog.New(slog.NewTextHandler(&logOutput, &slog.HandlerOptions{
						ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
							// Remove time for deterministic output
							if a.Key == slog.TimeKey {
								return slog.Attr{}
							}
							return a
						},
					}))
					logger.LogAttrs(ctx, slog.LevelInfo, "CANONICAL-LOG-LINE",
						canonlog.Attrs(ctx)...)
				}()

				next.ServeHTTP(wrapped, r.WithContext(ctx))
			})
		}

		// Create handler that simulates some work
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			time.Sleep(150 * time.Millisecond) // Sleep is fine in synctest
			canonlog.Set(ctx, AttrUserID, "usr_456")
			w.WriteHeader(http.StatusOK)
		})

		// Execute the request
		req := httptest.NewRequest("GET", "/api/users", nil)
		rec := httptest.NewRecorder()
		middleware(handler).ServeHTTP(rec, req)

		// Verify the log output
		want := `level=INFO msg=CANONICAL-LOG-LINE http_method=GET http_path=/api/users user_id=usr_456 http_status=200 duration=150ms` + "\n"
		if got := logOutput.String(); got != want {
			t.Errorf("log output:\ngot:  %q\nwant: %q", got, want)
		}
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
