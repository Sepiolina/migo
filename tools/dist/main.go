// dist builds a ready-to-test service folder for Linux or Windows:
//
//	go run ./tools/dist                       # asks which target
//	go run ./tools/dist -os linux             # dist/maiservice-linux-amd64/
//	go run ./tools/dist -os windows           # dist/maiservice-windows-amd64/
//	go run ./tools/dist -os all -arch arm64
//
// The folder holds maiservice and migo, a PM2 ecosystem.config.js, start and
// smoke-test scripts, sample inputs and a config.json for a local test (DB and
// SAP off). Rebuilding replaces the generated files but keeps config.json,
// uploads/, outputs/ and workflows/, so local edits and results survive.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "dist:", err)
		os.Exit(1)
	}
}

type target struct{ OS, Arch string }

func (t target) exe(name string) string {
	if t.OS == "windows" {
		return name + ".exe"
	}
	return name
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("dist", flag.ContinueOnError)
	goos := flags.String("os", "", "target: linux, windows or all (asks when omitted)")
	arch := flags.String("arch", "amd64", "CPU architecture: amd64 or arm64")
	out := flags.String("out", "dist", "parent folder for the build")
	port := flags.Int("port", 3081, "port written into a new config.json and ecosystem.config.js")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q (use -os linux|windows|all)", flags.Arg(0))
	}
	if _, err := os.Stat(filepath.Join("cmd", "maiservice")); err != nil {
		return errors.New("run this from the migo repository folder")
	}
	if *arch != "amd64" && *arch != "arm64" {
		return fmt.Errorf("-arch %q: use amd64 or arm64", *arch)
	}
	if *port < 1 || *port > 65535 {
		return fmt.Errorf("-port %d: use 1-65535", *port)
	}
	if *goos == "" {
		var err error
		if *goos, err = ask(stdin, stdout); err != nil {
			return err
		}
	}
	var targets []target
	switch *goos {
	case "linux", "windows":
		targets = []target{{*goos, *arch}}
	case "all":
		targets = []target{{"linux", *arch}, {"windows", *arch}}
	default:
		return fmt.Errorf("-os %q: use linux, windows or all", *goos)
	}
	for _, t := range targets {
		dir := filepath.Join(*out, fmt.Sprintf("maiservice-%s-%s", t.OS, t.Arch))
		if err := build(t, dir, *port, stdout); err != nil {
			return fmt.Errorf("%s/%s: %w", t.OS, t.Arch, err)
		}
	}
	return nil
}

// ask reads the target from stdin (a terminal or a pipe) when -os is not given.
func ask(stdin io.Reader, stdout io.Writer) (string, error) {
	fmt.Fprint(stdout, "Build for which system?\n  1) linux\n  2) windows\n  3) both\nChoice [1-3]: ")
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", errors.New("no choice read; use -os linux|windows|all")
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "1", "linux", "l":
		return "linux", nil
	case "2", "windows", "win", "w":
		return "windows", nil
	case "3", "both", "all", "b":
		return "all", nil
	}
	return "", fmt.Errorf("unknown choice %q", strings.TrimSpace(line))
}

func build(t target, dir string, port int, stdout io.Writer) error {
	fmt.Fprintf(stdout, "Building %s/%s into %s\n", t.OS, t.Arch, dir)
	if err := os.MkdirAll(filepath.Join(dir, "samples"), 0o755); err != nil {
		return err
	}
	for _, cmd := range []string{"maiservice", "migo"} {
		if err := goBuild(t, filepath.Join(dir, t.exe(cmd)), "./cmd/"+cmd); err != nil {
			return err
		}
	}
	files := map[string]string{
		"ecosystem.config.js": ecosystem(t, port),
		"README.txt":          readme(t, port),
	}
	if t.OS == "windows" {
		files["start.cmd"] = startCmd
		files["smoke-test.ps1"] = fmt.Sprintf(smokePS1, port)
	} else {
		files["start.sh"] = startSh
		files["smoke-test.sh"] = fmt.Sprintf(smokeSh, port)
	}
	for name, body := range files {
		if t.OS == "windows" {
			body = strings.ReplaceAll(body, "\n", "\r\n")
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, ".sh") {
			mode = 0o755
		}
		if err := writeFile(filepath.Join(dir, name), []byte(body), mode); err != nil {
			return err
		}
	}
	for src, dst := range map[string]string{
		"config.example.json":      "config.example.json",
		"testdata/bom_basic.xlsx":  "samples/bom_basic.xlsx",
		"testdata/prio_basic.xlsx": "samples/prio_basic.xlsx",
		"workflows/bom.yaml":       "samples/bom.yaml",
	} {
		b, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := writeFile(filepath.Join(dir, dst), b, 0o644); err != nil {
			return err
		}
	}
	cfg := filepath.Join(dir, "config.json")
	if _, err := os.Stat(cfg); errors.Is(err, os.ErrNotExist) {
		if err := writeFile(cfg, []byte(fmt.Sprintf(localConfig, port)), 0o600); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "  wrote config.json (DB and SAP off, for a local test)")
	} else {
		fmt.Fprintln(stdout, "  kept existing config.json")
	}
	fmt.Fprintf(stdout, "  done: %s\n", dir)
	return nil
}

func goBuild(t target, out, pkg string) error {
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", out, pkg)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+t.OS, "GOARCH="+t.Arch)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build %s: %w", pkg, err)
	}
	return nil
}

// writeFile replaces path through a temporary file, so a running service
// never sees a half-written file.
func writeFile(path string, b []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func ecosystem(t target, port int) string {
	return fmt.Sprintf(`// PM2 process file. Start from this folder with:  pm2 start ecosystem.config.js
// Paths are relative to this file, so the folder can be copied anywhere.
const path = require('path');

module.exports = {
  apps: [{
    name: 'maiservice',
    script: path.join(__dirname, %q),
    cwd: __dirname,
    interpreter: 'none',
    args: ['-config', path.join(__dirname, 'config.json')],
    exec_mode: 'fork',
    instances: 1,            // keep 1: download state and file registries belong to one process
    autorestart: true,
    min_uptime: '10s',
    max_restarts: 10,
    exp_backoff_restart_delay: 1000,
    max_memory_restart: '650M',
    kill_timeout: 15000,     // time for the graceful shutdown
    time: true,
    env: { PORT: '%d', GOMEMLIMIT: '400MiB' }
  }]
};
`, t.exe("maiservice"), port)
}

const localConfig = `{
  "port": %d,

  "db_enabled": false,
  "db_server": "sql-server-host",
  "db_port": 1433,
  "db_user": "user",
  "db_password": "change-me",
  "db_name": "SAP_ForthFacDb",
  "db_tls_min": "1.0",

  "sap_enabled": false,
  "sap_host": "sap-gateway-host",
  "sap_port": 5000,
  "sap_tcode": "CHANGE_ME",
  "sap_werks": "CHANGE_ME",
  "api_batch_size": 50,
  "api_retries": 3
}
`

const startSh = `#!/bin/sh
# Runs the service in the foreground (Ctrl+C stops it). For PM2 use ecosystem.config.js.
cd "$(dirname "$0")" || exit 1
exec ./maiservice -config ./config.json
`

const startCmd = `@echo off
rem Runs the service in the foreground (Ctrl+C stops it). For PM2 use ecosystem.config.js.
cd /d "%~dp0"
maiservice.exe -config config.json
`

const smokeSh = `#!/bin/sh
# Checks a running service: health, a BOM upload with the samples, the download.
set -e
cd "$(dirname "$0")"
URL="${URL:-http://localhost:%d}"
echo "health:"; curl -fsS "$URL/api/health"; echo
echo "upload:"; curl -fsS -F "files=@samples/bom_basic.xlsx" -F "files=@samples/prio_basic.xlsx" -F "bam=113280" "$URL/api/bom/upload"; echo
curl -fsS -o smoke-output.xlsx "$URL/api/bom/download"
echo "download: saved smoke-output.xlsx ($(wc -c < smoke-output.xlsx) bytes)"
`

const smokePS1 = `# Checks a running service: health, a BOM upload with the samples, the download.
# Run: powershell -ExecutionPolicy Bypass -File .\smoke-test.ps1
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
$url = if ($env:URL) { $env:URL } else { 'http://localhost:%d' }
Write-Host 'health:'; curl.exe -fsS "$url/api/health"; Write-Host ''
if ($LASTEXITCODE) { throw 'health check failed' }
Write-Host 'upload:'; curl.exe -fsS -F "files=@samples/bom_basic.xlsx" -F "files=@samples/prio_basic.xlsx" -F "bam=113280" "$url/api/bom/upload"; Write-Host ''
if ($LASTEXITCODE) { throw 'upload failed' }
curl.exe -fsS -o smoke-output.xlsx "$url/api/bom/download"
if ($LASTEXITCODE) { throw 'download failed' }
Write-Host ("download: saved smoke-output.xlsx ({0} bytes)" -f (Get-Item smoke-output.xlsx).Length)
`

func readme(t target, port int) string {
	start, smoke, migo := "./start.sh", "./smoke-test.sh", "./migo"
	if t.OS == "windows" {
		start, smoke, migo = `.\start.cmd`, `powershell -ExecutionPolicy Bypass -File .\smoke-test.ps1`, `.\migo.exe`
	}
	return fmt.Sprintf(`MaiService test build: %[1]s/%[2]s
=====================================

Contents
  %[3]s          the HTTP service
  %[4]s                the workflow CLI (no HTTP)
  config.json         local test settings: DB and SAP off. Edit for real servers;
                      a rebuild keeps this file. config.example.json has every key.
  ecosystem.config.js PM2 process file (paths relative to this folder)
  samples/            a small BOM and Priority workbook for the smoke test, and
                      bom.yaml (a copy of the built-in BOM workflow, for migo)
  uploads/, outputs/  created by the service on first start

1. Run it in the foreground
     %[5]s
   Health: http://localhost:%[6]d/api/health   (Ctrl+C stops it)

2. Or run it under PM2 (needs Node.js and pm2)
     pm2 start ecosystem.config.js
     pm2 logs maiservice
     pm2 stop maiservice

3. Smoke test, in a second terminal while the service runs
     %[7]s
   Expect health "online", upload "success", and smoke-output.xlsx.

4. Run a workflow without the service
     %[8]s -in bom=samples/bom_basic.xlsx -in priority=samples/prio_basic.xlsx -out out.xlsx samples/bom.yaml

Workflows are built into the binary. To override one, put its .yaml file in a
workflows/ folder here and restart. To use DB and SAP, set db_enabled and
sap_enabled to true in config.json with real values.
`, t.OS, t.Arch, t.exe("maiservice"), t.exe("migo"), start, port, smoke, migo)
}
