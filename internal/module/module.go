// Package module defines what a feature module is and what it is given.
//
// To add a feature: create internal/modules/<name>, give it a New(Services)
// constructor and a Routes method, and list it in internal/app/modules.go.
// docs/EXTENDING.md walks through it with code.
package module

import (
	"context"

	"miservice/internal/config"
	"miservice/internal/httpx"
	"miservice/internal/jobs"
	"miservice/internal/logx"
	"miservice/internal/storage"
)

// Services is everything shared that a module may use. A module receives it
// in its constructor and keeps what it needs; it never reaches for globals.
type Services struct {
	Config    *config.Config
	Log       logx.Logger
	Store     *storage.OutputStore // generated files with retention/cleanup
	UploadDir string               // where httpx.ParseUpload should store uploads
	Uploads   *storage.UploadStore // keep registry: unregistered uploads are auto-deleted
	Gate      *jobs.Gate           // bounds concurrent heavy jobs across modules
}

// Module is one feature of the service.
type Module interface {
	// Name identifies the module in logs.
	Name() string
	// Routes registers the module's endpoints.
	Routes(r *httpx.Router)
}

// Starter is optional: a module that runs background work implements it.
// Start is called once after all routes are registered and must return
// quickly (launch goroutines); ctx is cancelled at shutdown.
type Starter interface {
	Start(ctx context.Context) error
}

// Closer is optional: a module that holds resources (connections, files)
// implements it; Close is called once after the HTTP server has stopped.
type Closer interface{ Close() error }

// Factory builds a module from the shared services. A failing factory stops
// startup with a clear error.
type Factory func(Services) (Module, error)
