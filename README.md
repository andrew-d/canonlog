# canonlog

A minimal Go library for emitting canonical log lines.

A canonical log line is a single structured log entry emitted at the end of
each request that aggregates all relevant information about that request. This
pattern simplifies debugging, since you no longer need to join multiple log
lines together to understand what happened during a request, and can be used
for analytics (by shipping log lines to a data warehouse).

## Installation

```
go get github.com/andrew-d/canonlog
```

## Usage

```go
package main

import (
	"context"
	"log/slog"

	"github.com/andrew-d/canonlog"
)

var (
	AttrUserID = canonlog.Register[string]("user_id")
	AttrStatus = canonlog.Register[int]("status")
)

func main() {
	ctx := canonlog.New(context.Background())

	canonlog.Set(ctx, AttrUserID, "usr_123")
	canonlog.Set(ctx, AttrStatus, 200)

	slog.LogAttrs(ctx, slog.LevelInfo, "canonical-log-line", canonlog.Attrs(ctx)...)
}
```

## Documentation

See [pkg.go.dev](https://pkg.go.dev/github.com/andrew-d/canonlog) for full API documentation.

## Prior Art

- [Stripe's original Canonical Log Lines blog post](https://stripe.com/blog/canonical-log-lines)
- [Using Canonical Log Lines for Online Visibility](https://brandur.org/canonical-log-lines) by @brandur
- [Canonical Log Lines 2.0](https://brandur.org/nanoglyphs/025-logs) also by @brandur
