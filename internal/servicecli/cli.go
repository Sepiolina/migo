// Package servicecli owns the shared server command-line entry point.
package servicecli

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"miservice/internal/app"
)

func defaultConfigPath() string {
	if p := os.Getenv("MAISERVICE_CONFIG"); p != "" {
		return p
	}
	// Compatibility for existing deployments.
	if p := os.Getenv("MISERVICE_CONFIG"); p != "" {
		return p
	}
	if _, err := os.Stat("config.json"); err == nil {
		return "config.json"
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "config.json")
	}
	return "config.json"
}

// Main parses server flags and runs the service until shutdown.
func Main() {
	cfgPath := flag.String("config", defaultConfigPath(), "path to config.json (uploads/ and outputs/ are created beside it)")
	flag.Parse()
	if err := app.Run(*cfgPath); err != nil {
		log.Fatal(err)
	}
}
