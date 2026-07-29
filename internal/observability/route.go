package observability

import (
	"net/http"
	"runtime"

	"github.com/go-chi/chi/v5"
)

func routePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if pattern := rctx.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return r.URL.Path
}

func stackTrace() string {
	const maxStack = 8 << 10
	buf := make([]byte, maxStack)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}
