package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

// Run serves until ctx is cancelled, then shuts down gracefully.
func (a *App) Run(ctx context.Context) error {
	var ln net.Listener
	var err error
	if os.Getenv("MAISERVICE_LISTEN_FD") == "3" {
		f := os.NewFile(3, "inherited-listener")
		ln, err = net.FileListener(f)
		f.Close()
	} else {
		ln, err = net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", a.cfg.Port))
	}
	if err != nil {
		a.closeModules()
		return err
	}
	return a.serve(ctx, ln)
}

// serve owns the listener and module lifetime, including failed startup.
func (a *App) serve(ctx context.Context, ln net.Listener) error {
	ctx, cancel := context.WithCancel(ctx)
	defer a.closeModules()
	defer cancel()
	defer ln.Close()
	hs := &http.Server{
		Addr:              fmt.Sprintf("0.0.0.0:%d", a.cfg.Port),
		Handler:           a.Handler(),
		ReadHeaderTimeout: 30 * time.Second,
	}
	if err := a.start(ctx); err != nil {
		return err
	}
	a.printBanner()

	errc := make(chan error, 1)
	go func() { errc <- hs.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	fmt.Println("\nclosing server gracefully...")
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shutErr := hs.Shutdown(sctx)
	if shutErr != nil {
		// Shutdown leaves active connections open when its deadline expires.
		_ = hs.Close()
	}
	fmt.Println("Server closed.")
	return shutErr
}
