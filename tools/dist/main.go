// dist builds a ready-to-test service folder for Linux or Windows:
//
//	go run ./tools/dist                       # asks which target
//	go run ./tools/dist -os linux             # dist/maiservice-linux-amd64/
//	go run ./tools/dist -os windows           # dist/maiservice-windows-amd64/
//	go run ./tools/dist -os all -arch arm64 -cli
//
// The folder holds three files: maiservice, config.json (DB and SAP off, for
// a local test) and an ecosystem.config.js for PM2 that works on Linux and
// Windows alike. The port lives only in config.json. Add -cli to also build
// the migo CLI. Rebuilding replaces the binaries and ecosystem.config.js but
// keeps config.json, uploads/, outputs/ and workflows/.
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
	port := flags.Int("port", 3081, "port written into a new config.json (an existing one is kept)")
	cli := flags.Bool("cli", false, "also build the migo workflow CLI")
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
		if err := build(t, dir, *port, *cli, stdout); err != nil {
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

func build(t target, dir string, port int, cli bool, stdout io.Writer) error {
	fmt.Fprintf(stdout, "Building %s/%s into %s\n", t.OS, t.Arch, dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	cmds := []string{"maiservice"}
	if cli {
		cmds = append(cmds, "migo")
	}
	for _, cmd := range cmds {
		if err := goBuild(t, filepath.Join(dir, t.exe(cmd)), "./cmd/"+cmd); err != nil {
			return err
		}
	}
	// Files that older versions of this tool generated.
	for _, old := range []string{"README.txt", "config.example.json", "start.sh", "start.cmd", "smoke-test.sh", "smoke-test.ps1", "smoke-output.xlsx", "samples"} {
		if err := os.RemoveAll(filepath.Join(dir, old)); err != nil {
			return err
		}
	}
	if !cli {
		_ = os.Remove(filepath.Join(dir, t.exe("migo")))
	}
	if err := writeFile(filepath.Join(dir, "ecosystem.config.js"), []byte(ecosystem), 0o644); err != nil {
		return err
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

// ecosystem is the same file for every target: it picks maiservice.exe on
// Windows and maiservice elsewhere, and leaves the port to config.json.
const ecosystem = `// PM2: pm2 start ecosystem.config.js   (the port is set in config.json)
const path = require('path');
const exe = process.platform === 'win32' ? 'maiservice.exe' : 'maiservice';

module.exports = {
  apps: [{
    name: 'maiservice',
    script: path.join(__dirname, exe),
    args: ['-config', path.join(__dirname, 'config.json')],
    cwd: __dirname,
    interpreter: 'none',
    instances: 1,            // keep 1: download state and file registries belong to one process
    autorestart: true,
    min_uptime: '10s',
    max_restarts: 10,
    exp_backoff_restart_delay: 1000,
    max_memory_restart: '650M',
    kill_timeout: 15000,     // time for the graceful shutdown
    time: true,
    env: { GOMEMLIMIT: '400MiB' }
  }]
};
`

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
