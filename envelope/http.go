package envelope

import (
	"encoding/json"
	"net/http"
)

// WriteJSON writes the envelope as JSON to the response writer with the given status code.
// Sets Content-Type: application/json.
func WriteJSON(w http.ResponseWriter, status int, env *Envelope) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(env)
}

// statusToCode maps HTTP status codes to error codes.
var statusToCode = map[int]string{
	http.StatusBadRequest:          BadRequest,
	http.StatusUnauthorized:        Unauthorized,
	http.StatusForbidden:           Forbidden,
	http.StatusNotFound:            NotFound,
	http.StatusConflict:            Conflict,
	http.StatusTooManyRequests:     RateLimited,
	http.StatusInternalServerError: InternalError,
	http.StatusGatewayTimeout:      ExecutionTimeout,
	http.StatusServiceUnavailable:  PolicyUnavailable,
}

// FromHTTPStatus creates an Envelope from an HTTP status code and message.
// Uses the mapping table above; defaults to INTERNAL_ERROR for unknown statuses.
func FromHTTPStatus(status int, message string) *Envelope {
	code, ok := statusToCode[status]
	if !ok {
		code = InternalError
	}
	retryable := status == http.StatusServiceUnavailable ||
		status == http.StatusGatewayTimeout ||
		status == http.StatusTooManyRequests
	return New(code, message, WithRetryable(retryable))
}
