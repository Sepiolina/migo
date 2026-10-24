// Package system holds the service-level endpoints that belong to no
// feature (currently just the health check). It is also the smallest
// complete example of a module: copy it when starting a new one.
package system

import (
	"net/http"

	"miservice/internal/httpx"
	"miservice/internal/module"
)

// Module serves GET /api/health.
type Module struct{}

// New is the module.Factory.
func New(module.Services) (module.Module, error) { return &Module{}, nil }

func (*Module) Name() string { return "system" }

func (m *Module) Routes(r *httpx.Router) {
	r.GET("/api/health", m.health)
}

// healthBody is a struct (not a map) so key order matches the Express output.
type healthBody struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

func (*Module) health(w http.ResponseWriter, _ *http.Request) error {
	return httpx.WriteJSON(w, http.StatusOK, healthBody{"online", "BOM Processor Node.js API"})
}
