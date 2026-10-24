package httpx

import (
	"net/http"

	"miservice/internal/logx"
)

// Middleware wraps an http.Handler.
type Middleware func(next http.Handler) http.Handler

// CORS ports Express' cors() defaults: any origin, preflight answered with
// 204 before routing.
func CORS() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", "*")
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "GET,HEAD,PUT,PATCH,POST,DELETE")
				if reqH := r.Header.Get("Access-Control-Request-Headers"); reqH != "" {
					h.Set("Access-Control-Allow-Headers", reqH)
					h.Add("Vary", "Access-Control-Request-Headers")
				}
				h.Set("Content-Length", "0")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Recover turns a panic inside a handler into a logged 500 JSON response
// instead of a dropped connection, so one bad request (or a bug in a newly
// added endpoint) never looks like the service died.
func Recover(log logx.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				log.Printf("ERROR: panic serving %s %s: %v", r.Method, r.URL.Path, rec)
				_ = WriteJSON(w, http.StatusInternalServerError, errorBody{"error", "Internal Server Error"})
			}()
			next.ServeHTTP(w, r)
		})
	}
}
