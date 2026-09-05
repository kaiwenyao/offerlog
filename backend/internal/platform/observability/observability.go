package observability

import (
	"context"
	"log/slog"
	"os"
)

var logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

func Init(level string) {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}

func L(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if id := RequestID(ctx); id != "" {
			return logger.With("request_id", id)
		}
	}
	return logger
}

func Log() *slog.Logger { return logger }

type ctxKey int

const ridKey ctxKey = 0

// WithRequestID returns a context carrying the given request id.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ridKey, id)
}

// RequestID returns the request id stored in ctx, if any.
func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(ridKey).(string); ok {
		return v
	}
	return ""
}
