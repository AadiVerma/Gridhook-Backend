package observability

import "context"

type ctxKey int

const (
	requestIDKey ctxKey = iota
	orgIDKey
)

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

func WithOrgID(ctx context.Context, orgID int64) context.Context {
	return context.WithValue(ctx, orgIDKey, orgID)
}

func OrgIDFromContext(ctx context.Context) int64 {
	orgID, _ := ctx.Value(orgIDKey).(int64)
	return orgID
}
