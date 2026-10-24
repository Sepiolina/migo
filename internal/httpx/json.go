package httpx

import (
	"net/http"

	"miservice/internal/jsx"
)

// WriteJSON writes v as a JSON response with the given status. Struct
// fields keep their declaration order, so response bodies are declared as
// structs (not maps) wherever key order matters to a client.
func WriteJSON(w http.ResponseWriter, status int, v any) error {
	b, err := jsx.Stringify(v)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, err = w.Write(b)
	return err
}
