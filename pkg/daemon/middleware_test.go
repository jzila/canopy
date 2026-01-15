package daemon

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jzila/canopy/pkg/logging"
)

func TestGenerateRequestID(t *testing.T) {
	// Generate multiple IDs and check they're unique and valid
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := generateRequestID()
		if len(id) != 16 {
			t.Errorf("expected request ID length 16, got %d", len(id))
		}
		if seen[id] {
			t.Errorf("duplicate request ID generated: %s", id)
		}
		seen[id] = true
	}
}

func TestRequestIDMiddleware_GeneratesID(t *testing.T) {
	// Handler that captures the request ID from context
	var capturedRequestID string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequestID = logging.RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with middleware
	wrapped := RequestIDMiddleware(handler)

	// Make request without X-Request-ID header
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// Check that a request ID was generated
	if capturedRequestID == "" {
		t.Error("expected request ID to be generated")
	}

	// Check that it's in the response header
	responseID := rec.Header().Get(RequestIDHeader)
	if responseID == "" {
		t.Error("expected X-Request-ID in response header")
	}
	if responseID != capturedRequestID {
		t.Errorf("response header (%s) doesn't match context (%s)", responseID, capturedRequestID)
	}
}

func TestRequestIDMiddleware_UsesProvidedID(t *testing.T) {
	providedID := "my-custom-request-id-123"

	// Handler that captures the request ID from context
	var capturedRequestID string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequestID = logging.RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with middleware
	wrapped := RequestIDMiddleware(handler)

	// Make request with X-Request-ID header
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(RequestIDHeader, providedID)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// Check that the provided ID was used
	if capturedRequestID != providedID {
		t.Errorf("expected request ID %q, got %q", providedID, capturedRequestID)
	}

	// Check that it's in the response header
	responseID := rec.Header().Get(RequestIDHeader)
	if responseID != providedID {
		t.Errorf("expected response header %q, got %q", providedID, responseID)
	}
}

func TestLoggingMiddleware(t *testing.T) {
	// Handler that returns 201 Created
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("created"))
	})

	// Wrap with middleware
	wrapped := LoggingMiddleware(handler)

	// Make request
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// Check response
	if rec.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, rec.Code)
	}
}

func TestMiddlewareChain(t *testing.T) {
	// Test that the full middleware chain works together
	var capturedRequestID string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequestID = logging.RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	// Apply middleware chain in the same order as the server
	wrapped := RequestIDMiddleware(LoggingMiddleware(handler))

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// Verify request ID was generated and is in context
	if capturedRequestID == "" {
		t.Error("expected request ID in context")
	}

	// Verify it's in the response header
	if rec.Header().Get(RequestIDHeader) == "" {
		t.Error("expected X-Request-ID in response header")
	}
}

func TestResponseWriter_WriteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec, statusCode: http.StatusOK}

	rw.WriteHeader(http.StatusNotFound)

	if rw.statusCode != http.StatusNotFound {
		t.Errorf("expected status code %d, got %d", http.StatusNotFound, rw.statusCode)
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected recorded code %d, got %d", http.StatusNotFound, rec.Code)
	}
}
