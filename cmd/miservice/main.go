//go:debug tlsrsakex=1

// miservice: BOM processor service (Go port of the Node/Express service).
// All wiring lives in internal/app; features live in internal/modules.
package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"miservice/internal/app"
)

func defaultConfigPath() string {
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

func main() {
	cfgPath := flag.String("config", defaultConfigPath(), "path to config.json (uploads/ and outputs/ are created beside it)")
	flag.Parse()
	if err := app.Run(*cfgPath); err != nil {
		log.Fatal(err)
	}
}
