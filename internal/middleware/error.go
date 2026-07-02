package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// APIError represents a structured, operational HTTP error.
type APIError struct {
	Message    string `json:"message"`
	Code       string `json:"code"`
	StatusCode int    `json:"statusCode"`
	Details    string `json:"details,omitempty"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	return e.Message
}

// NewAPIError creates a new APIError.
func NewAPIError(message, code string, statusCode int) *APIError {
	return &APIError{Message: message, Code: code, StatusCode: statusCode}
}

// NewValidationError creates a 400 validation error.
func NewValidationError(message string) *APIError {
	return NewAPIError(message, "VALIDATION_ERROR", http.StatusBadRequest)
}

// NewServiceError creates a 503 service unavailable error.
func NewServiceError(service string) *APIError {
	return NewAPIError(service+" service is currently unavailable", "SERVICE_UNAVAILABLE", http.StatusServiceUnavailable)
}

// NewTimeoutError creates a 504 timeout error.
func NewTimeoutError(operation string) *APIError {
	return NewAPIError(operation+" timed out", "TIMEOUT", http.StatusGatewayTimeout)
}

// ErrorResponse is the JSON envelope returned to clients.
type ErrorResponse struct {
	Success   bool      `json:"success"`
	Error     APIError  `json:"error"`
	Timestamp time.Time `json:"timestamp"`
	RequestID string    `json:"requestId,omitempty"`
}

// ErrorHandler returns middleware that recovers from panics and renders a
// consistent JSON error response. Handlers can short-circuit by writing
// their own responses; this middleware only activates when the response has
// not yet been written.
func ErrorHandler() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					msg := "Internal server error"
					if err, ok := rec.(error); ok {
						msg = err.Error()
					} else {
						msg = http.StatusText(http.StatusInternalServerError)
					}
					writeErrorResponse(w, r, NewAPIError(msg, "INTERNAL_ERROR", http.StatusInternalServerError))
				}
			}()
			rw := &errorResponseWriter{ResponseWriter: w}
			next.ServeHTTP(rw, r)
			if !rw.written && rw.statusCode >= 400 {
				apiErr := classifyStatusError(rw.statusCode)
				writeErrorResponse(w, r, apiErr)
			}
		})
	}
}

// NotFoundHandler returns a handler that produces a consistent 404 response.
func NotFoundHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(w, r, NewAPIError(
			"Route "+r.Method+" "+r.URL.Path+" not found",
			"ROUTE_NOT_FOUND",
			http.StatusNotFound,
		))
	}
}

// writeErrorResponse writes the standardized error envelope.
func writeErrorResponse(w http.ResponseWriter, r *http.Request, apiErr *APIError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(apiErr.StatusCode)
	_ = json.NewEncoder(w).Encode(ErrorResponse{
		Success:   false,
		Error:     *apiErr,
		Timestamp: time.Now().UTC(),
		RequestID: GetRequestID(r),
	})
}

func classifyStatusError(status int) *APIError {
	switch status {
	case http.StatusBadRequest:
		return NewAPIError("Bad request", "BAD_REQUEST", status)
	case http.StatusUnauthorized:
		return NewAPIError("Authentication required", "UNAUTHORIZED", status)
	case http.StatusForbidden:
		return NewAPIError("Forbidden", "FORBIDDEN", status)
	case http.StatusNotFound:
		return NewAPIError("Not found", "NOT_FOUND", status)
	case http.StatusMethodNotAllowed:
		return NewAPIError("Method not allowed", "METHOD_NOT_ALLOWED", status)
	case http.StatusTooManyRequests:
		return NewAPIError("Too many requests", "RATE_LIMITED", status)
	case http.StatusServiceUnavailable:
		return NewAPIError("Service unavailable", "SERVICE_UNAVAILABLE", status)
	case http.StatusGatewayTimeout:
		return NewAPIError("Gateway timeout", "TIMEOUT", status)
	default:
		return NewAPIError("Internal server error", "INTERNAL_ERROR", status)
	}
}

// errorResponseWriter wraps http.ResponseWriter and tracks whether a response
// has been written and which status code was set.
type errorResponseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *errorResponseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.written = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *errorResponseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.written = true
	}
	return rw.ResponseWriter.Write(b)
}

// IsAPIError reports whether err is an *APIError.
func IsAPIError(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr)
}

// AsAPIError returns err as *APIError if it is one, otherwise nil.
func AsAPIError(err error) *APIError {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return nil
}
