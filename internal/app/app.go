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
	"strings"

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

// localIP is the address other machines should use: the one the system
// routes outbound traffic from, else the best interface address. Link-local
// 169.254.x.x addresses (an adapter without a network, common on Windows)
// are never picked; nobody else can reach them.
func localIP() string {
	return pickIP(routeIP(), interfaceIPs())
}

// otherIPs lists the remaining usable addresses, for the banner.
func otherIPs(chosen string) []string {
	var out []string
	for _, ip := range rankIPs(interfaceIPs()) {
		if s := ip.String(); s != chosen {
			out = append(out, s)
		}
	}
	return out
}

// routeIP asks the OS which local address it would use to reach a public
// address. UDP "connect" only selects a route; no packet is sent.
func routeIP() net.IP {
	c, err := net.Dial("udp4", "192.0.2.1:9") // TEST-NET-1, never routed on the internet
	if err != nil {
		return nil
	}
	defer c.Close()
	if a, ok := c.LocalAddr().(*net.UDPAddr); ok {
		return a.IP
	}
	return nil
}

// interfaceIPs returns the IPv4 addresses of interfaces that are up.
func interfaceIPs() []net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []net.IP
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				if ip4 := ipn.IP.To4(); ip4 != nil {
					out = append(out, ip4)
				}
			}
		}
	}
	return out
}

func usable(ip net.IP) bool {
	return ip != nil && ip.To4() != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified()
}

// rankIPs keeps the usable addresses, private LAN ranges first, in order.
func rankIPs(ips []net.IP) []net.IP {
	var lan, other []net.IP
	for _, ip := range ips {
		switch {
		case !usable(ip):
		case ip.IsPrivate():
			lan = append(lan, ip)
		default:
			other = append(other, ip)
		}
	}
	return append(lan, other...)
}

func pickIP(route net.IP, ips []net.IP) string {
	if usable(route) {
		return route.String()
	}
	if r := rankIPs(ips); len(r) > 0 {
		return r[0].String()
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
	if others := otherIPs(ip); len(others) > 0 {
		fmt.Printf(" Other addresses: %s\n", strings.Join(others, ", "))
	}
	fmt.Printf("--------------------------------------------------\n")
	fmt.Printf(" REST Endpoints:\n")
	for _, r := range a.router.Routes() {
		fmt.Printf("   %-5s %s://%s:%d%s\n", r.Method, "http", ip, port, r.Path)
	}
	fmt.Printf("==================================================\n\n")
}
