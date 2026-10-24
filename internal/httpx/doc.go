// Package httpx is the small HTTP toolkit every module builds its API on.
//
// A handler is a plain function that RETURNS an error:
//
//	func (m *Module) hello(w http.ResponseWriter, r *http.Request) error {
//		return httpx.WriteJSON(w, 200, map[string]string{"hi": "there"})
//	}
//
// Returning an error is the only way a handler reports failure; the router
// turns it into the JSON error response (see errors.go), so no handler ever
// formats an error body by hand.
//
// Routes are registered on a Router (router.go). Cross-cutting behaviour
// (CORS, panic recovery) is middleware (middleware.go). File uploads go
// through ParseUpload (upload.go).
package httpx
