// Package app is the composition root: it builds the shared services, creates
// every registered module, mounts their routes and runs the HTTP server.
package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"miservice/internal/config"
	"miservice/internal/httpx"
	"miservice/internal/jobs"
	"miservice/internal/logx"
	"miservice/internal/module"
	"miservice/internal/storage"
)

const maxConcurrentJobs = 2 // heavy jobs at once, across modules (bounds RAM)

// App is a fully wired service, ready to serve.
type App struct {
	cfg     *config.Config
	log     logx.Logger
	store   *storage.OutputStore
	uploads *storage.UploadStore
	router  *httpx.Router
	modules []module.Module
}

// New creates the shared services and every module from factories.
func New(cfg *config.Config, log logx.Logger, factories []module.Factory) (*App, error) {
	uploadDir := filepath.Join(cfg.Dir, "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, err
	}
	store, err := storage.NewOutputStore(filepath.Join(cfg.Dir, "outputs"))
	if err != nil {
		return nil, err
	}
	uploads, err := storage.NewUploadStore(uploadDir)
	if err != nil {
		return nil, err
	}
	svc := module.Services{Config: cfg, Log: log, Store: store, UploadDir: uploadDir, Uploads: uploads, Gate: jobs.NewGate(maxConcurrentJobs)}

	a := &App{cfg: cfg, log: log, store: store, uploads: uploads, router: httpx.NewRouter(log)}
	a.router.Use(httpx.Recover(log), httpx.CORS())
	for _, f := range factories {
		m, err := f(svc)
		if err != nil {
			a.closeModules()
			return nil, fmt.Errorf("module setup failed: %w", err)
		}
		m.Routes(a.router)
		a.modules = append(a.modules, m)
	}
	return a, nil
}

// Handler is the root HTTP handler (used by tests and by Run).
func (a *App) Handler() http.Handler { return a.router.Handler() }

// start launches background work: the output cleanup and any module Starter.
func (a *App) start(ctx context.Context) error {
	stop := make(chan struct{})
	go func() { <-ctx.Done(); close(stop) }()
	a.store.StartCleanupScheduler(a.log, stop) // biweekly cleanup of old outputs
	a.uploads.StartScheduler(a.log, stop)      // hourly: unregistered uploads; biweekly: kept uploads
	for _, m := range a.modules {
		if s, ok := m.(module.Starter); ok {
			if err := s.Start(ctx); err != nil {
				return fmt.Errorf("module %s: %w", m.Name(), err)
			}
		}
	}
	return nil
}

// closeModules closes modules that hold resources, in reverse order.
func (a *App) closeModules() {
	for i := len(a.modules) - 1; i >= 0; i-- {
		if c, ok := a.modules[i].(module.Closer); ok {
			if err := c.Close(); err != nil {
				a.log.Printf("closing module %s: %v", a.modules[i].Name(), err)
			}
		}
	}
	a.modules = nil
}

func localIP() string {
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && ipn.IP.To4() != nil {
				return ipn.IP.String()
			}
		}
	}
	return "127.0.0.1"
}

func (a *App) printBanner() {
	ip, port := localIP(), a.cfg.Port
	fmt.Printf("\n==================================================\n")
	fmt.Printf("🚀 BOM Processor Service Running on Port %d\n", port)
	fmt.Printf("==================================================\n")
	fmt.Printf(" Local Access   : http://localhost:%d\n", port)
	fmt.Printf(" Network Access : http://%s:%d  <-- Share this IP!\n", ip, port)
	fmt.Printf("--------------------------------------------------\n")
	fmt.Printf(" REST Endpoints:\n")
	for _, r := range a.router.Routes() {
		fmt.Printf("   %-5s %s://%s:%d%s\n", r.Method, "http", ip, port, r.Path)
	}
	fmt.Printf("==================================================\n\n")
}
