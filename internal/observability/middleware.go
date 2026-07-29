package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

const HeaderRequestID = "X-Request-ID"

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderRequestID)
		if len(id) == 0 || len(id) > 128 || !isPrintableASCII(id) {
			id = newRequestID()
		}
		w.Header().Set(HeaderRequestID, id)
		next.ServeHTTP(w, r.WithContext(WithRequestID(r.Context(), id)))
	})
}

func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rec, r)

			level := slog.LevelInfo
			switch {
			case rec.status >= http.StatusInternalServerError:
				level = slog.LevelError
			case rec.status >= http.StatusBadRequest:
				level = slog.LevelWarn
			}

			logger.LogAttrs(r.Context(), level, "http request",
				slog.String("method", r.Method),

				slog.String("path", routePattern(r)),
				slog.Int("status", rec.status),
				slog.Int64("bytes", rec.written),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}

func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer recoverPanic(r.Context(), logger, w, r)
			next.ServeHTTP(w, r)
		})
	}
}

func recoverPanic(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	recovered := recover()
	if recovered == nil {
		return
	}

	if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
		panic(recovered)
	}

	logger.ErrorContext(ctx, "panic recovered",
		slog.Any("panic", recovered),
		slog.String("method", r.Method),
		slog.String("path", routePattern(r)),
		slog.String("stack", stackTrace()),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte(`{"error":{"code":"internal_error","message":"internal server error"}}`))
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	written     int64
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.wroteHeader = true
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(b)
	r.written += int64(n)
	return n, err
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func newRequestID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {

		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(buf)
}

func isPrintableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}
