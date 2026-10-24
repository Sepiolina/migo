package app

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"miservice/internal/config"
	"miservice/internal/logx"
)

// Run is the whole program: load config, build the app, serve until
// SIGINT/SIGTERM.
func Run(cfgPath string) error {
	log := logx.Stdout()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	a, err := New(cfg, log, DefaultModules)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return a.Run(ctx)
}
