// Package userusageanalytics owns the private request marker used to scope
// aggregate reads to the ordinary-user usage routes.
package userusageanalytics

import "context"

type requestMarkerKey struct{}

// Mark returns a context that permits the transparent user-usage aggregate
// fast path. The unexported key prevents unrelated callers from enabling it by
// accident.
func Mark(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestMarkerKey{}, struct{}{})
}

// IsMarked reports whether the request was explicitly scoped by the ordinary
// user usage-route middleware.
func IsMarked(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	_, ok := ctx.Value(requestMarkerKey{}).(struct{})
	return ok
}
