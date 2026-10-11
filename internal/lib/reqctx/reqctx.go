// Package reqctx carries per-request identity through the call stack in a
// context.Context, so services never see the HTTP request.
package reqctx

import "context"

type key int

const requestIDKey key = iota

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func RequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}
