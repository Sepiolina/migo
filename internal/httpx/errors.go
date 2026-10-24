package httpx

import (
	"errors"
	"net/http"
)

// Error is a handler failure that carries its HTTP status (and optionally
// its exact response body).
type Error struct {
	Status int
	// Body, when set, is sent as-is and the error is NOT logged (an
	// expected client mistake such as a missing field).
	Body any
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// Fail rejects the request with the service's legacy client-error shape
// {"error": "..."} and the given status. Not logged: use it for validation
// problems the caller can fix.
//
//	return httpx.Fail(400, "Missing 'bam'")
func Fail(status int, msg string) error {
	return &Error{Status: status, Body: map[string]string{"error": msg}, Err: errors.New(msg)}
}

// Status answers with a specific status but the standard failure envelope
// {"status":"error","message":"..."} and logs err.
//
//	return httpx.Status(400, errors.New("File too large"))
func Status(status int, err error) error {
	return &Error{Status: status, Err: err}
}

// Any other error returned by a handler becomes status 500 with the same
// standard envelope, logged as "ERROR: ...".

type errorBody struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// respondError is the single place that turns a handler's error into a
// response (the port of shared/errorHandler.js).
func (rt *Router) respondError(w http.ResponseWriter, err error) {
	var he *Error
	asHTTP := errors.As(err, &he)
	if asHTTP && he.Body != nil {
		_ = WriteJSON(w, he.Status, he.Body)
		return
	}
	rt.log.Printf("ERROR: %v", err)
	status := http.StatusInternalServerError
	if asHTTP {
		status = he.Status
	}
	_ = WriteJSON(w, status, errorBody{"error", err.Error()})
}
