// Package daemon provides HTTP middleware for the canopy daemon.
package daemon

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"

	"github.com/jzila/canopy/pkg/logging"
)

const (
	// RequestIDHeader is the HTTP header name for request IDs.
	// If a client provides this header, it will be used; otherwise a new ID is generated.
	RequestIDHeader = "X-Request-ID"
)

// generateRequestID creates a new random request ID (8 bytes = 16 hex chars)
func generateRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to a zero ID if random generation fails (should never happen)
		return "0000000000000000"
	}
	return hex.EncodeToString(b)
}

// RequestIDMiddleware adds request ID tracing to HTTP requests.
// It extracts the request ID from the X-Request-ID header if present,
// otherwise generates a new one. The request ID is:
// - Added to the request context for use in handlers and logging
// - Set in the response X-Request-ID header
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get or generate request ID
		requestID := r.Header.Get(RequestIDHeader)
		if requestID == "" {
			requestID = generateRequestID()
		}

		// Add request ID to response header
		w.Header().Set(RequestIDHeader, requestID)

		// Add request ID to request context
		ctx := logging.ContextWithRequestID(r.Context(), requestID)

		// Call next handler with updated context
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// LoggingMiddleware logs HTTP requests with structured fields.
// It should be used after RequestIDMiddleware to include the request ID in logs.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Create a response wrapper to capture status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Log request start
		logging.DebugContext(r.Context(), "http request started",
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
		)

		// Call next handler
		next.ServeHTTP(rw, r)

		// Log request completion
		logging.DebugContext(r.Context(), "http request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.statusCode,
		)
	})
}

// responseWriter wraps http.ResponseWriter to capture the status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Hijack implements http.Hijacker by delegating to the underlying ResponseWriter.
// This is required for WebSocket upgrades to work through the middleware chain.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("responseWriter: underlying ResponseWriter does not implement http.Hijacker")
}
