// enginepack builds the engine kit: one zip with the workflow engine module,
// the migo command line, the conformance suite, the fixtures the engine's
// tests use, the workflow documentation and instructions for a coding agent
// (kit/AGENTS.md) to add the engine to another project with output identical
// to migo's.
//
//	go run ./tools/enginepack [-out dist/mai-engine-<version>.zip] [-version v]
//
// The zip holds one folder, mai-engine/, with the repository's layout so the
// engine's tests find their fixtures. MANIFEST.json records the source commit
// and a SHA-256 for every file. The archive is deterministic: the same tree
// gives the same bytes.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

//go:embed kit
var kit embed.FS

// Root is the folder inside the zip.
const Root = "mai-engine"

// trees are copied whole (the files git tracks or would track); docs are
// single files. Paths are relative to the repository root.
var (
	trees = []string{"engine", "conformance", "workflows", "examples", "testdata"}
	docs  = []string{"docs/WORKFLOWS.md", "docs/STEP_REFERENCE.md", "docs/CLI.md", "docs/workflow.schema.json"}
)

func main() {
	out := flag.String("out", "", "zip to write (default dist/mai-engine-<version>.zip)")
	version := flag.String("version", "", "version recorded in MANIFEST.json (default: git describe)")
	repo := flag.String("repo", ".", "repository root")
	flag.Parse()
	path, err := Build(*repo, *out, *version)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERR", err)
		os.Exit(1)
	}
	fmt.Println("wrote", path)
}

// Manifest describes a kit.
type Manifest struct {
	Name        string            `json:"name"`
	Module      string            `json:"module"`
	Version     string            `json:"version"`
	Commit      string            `json:"commit,omitempty"`
	Modified    bool              `json:"modified"`
	GoVersion   string            `json:"go"`
	Conformance int               `json:"conformanceSuites"`
	Checks      []string          `json:"checks"`
	Files       map[string]string `json:"files"` // path in the zip -> sha256
}

// Build writes the kit for the repository at repo and returns its path.
func Build(repo, out, version string) (string, error) {
	repo, err := filepath.Abs(repo)
	if err != nil {
		return "", err
	}
	files, err := collect(repo)
	if err != nil {
		return "", err
	}
	commit, modified := gitState(repo)
	if version == "" {
		version = describe(repo)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}$`).MatchString(version) {
		return "", fmt.Errorf("version %q: use letters, digits and . _ + -", version)
	}
	if out == "" {
		out = filepath.Join(repo, "dist", "mai-engine-"+version+".zip")
	}
	m := Manifest{
		Name: "Mai workflow engine kit", Module: "github.com/Sepiolina/migo/engine",
		Version: version, Commit: commit, Modified: modified, GoVersion: goVersion(files["engine/go.mod"]),
		Checks: []string{"cd engine && go vet ./...", "cd engine && go test ./...", "cd engine && go run ./cmd/migo test ../conformance"},
		Files:  map[string]string{},
	}
	for name, data := range files {
		sum := sha256.Sum256(data)
		m.Files[name] = hex.EncodeToString(sum[:])
		if strings.HasPrefix(name, "conformance/") && strings.HasSuffix(name, ".test.yaml") {
			m.Conformance++
		}
	}
	if m.Conformance == 0 {
		return "", errors.New("no conformance suites found")
	}
	manifest, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	files["MANIFEST.json"] = append(manifest, '\n')
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return "", err
	}
	return out, writeZip(out, files)
}

// collect reads every file of the kit, keyed by its path in the kit.
func collect(repo string) (map[string][]byte, error) {
	files := map[string][]byte{}
	listed, err := gitFiles(repo, trees)
	if err != nil {
		return nil, err
	}
	for _, name := range append(listed, docs...) {
		data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(name)))
		if errors.Is(err, fs.ErrNotExist) {
			continue // listed by git but deleted in the working tree
		}
		if err != nil {
			return nil, err
		}
		files[name] = data
	}
	for _, need := range append([]string{"engine/go.mod", "engine/go.sum", "engine/cmd/migo/main.go"}, docs...) {
		if files[need] == nil {
			return nil, fmt.Errorf("%s is missing", need)
		}
	}
	err = fs.WalkDir(kit, "kit", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := kit.ReadFile(p)
		files[strings.TrimPrefix(p, "kit/")] = data
		return err
	})
	if err != nil {
		return nil, err
	}
	files["CLAUDE.md"] = []byte("Read AGENTS.md before changing anything: it is the integration guide for this engine kit.\n")
	// The kit's go.mod must not point outside the kit.
	if bytes.Contains(files["engine/go.mod"], []byte("replace ")) {
		return nil, errors.New("engine/go.mod has a replace directive; the kit must build on its own")
	}
	return files, nil
}

// gitFiles lists the files under dirs that git tracks or would track
// (ignored files, such as build output, are left out).
func gitFiles(repo string, dirs []string) ([]string, error) {
	args := append([]string{"ls-files", "-z", "--cached", "--others", "--exclude-standard", "--"}, dirs...)
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	raw, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	seen := map[string]bool{}
	var out []string
	for _, name := range strings.Split(string(raw), "\x00") {
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out, nil
}

func gitState(repo string) (commit string, modified bool) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repo
	if b, err := cmd.Output(); err == nil {
		commit = strings.TrimSpace(string(b))
	}
	cmd = exec.Command("git", "status", "--porcelain", "--", "engine", "conformance")
	cmd.Dir = repo
	if b, err := cmd.Output(); err == nil {
		modified = len(bytes.TrimSpace(b)) > 0
	}
	return commit, modified
}

func describe(repo string) string {
	cmd := exec.Command("git", "describe", "--tags", "--always", "--dirty", "--match", "engine/v*")
	cmd.Dir = repo
	b, err := cmd.Output()
	if err != nil {
		return "dev"
	}
	return strings.ReplaceAll(strings.TrimPrefix(strings.TrimSpace(string(b)), "engine/"), "/", "-")
}

func goVersion(mod []byte) string {
	if m := regexp.MustCompile(`(?m)^go (\S+)`).FindSubmatch(mod); m != nil {
		return string(m[1])
	}
	return ""
}

// writeZip writes files under Root/ in name order with a fixed time, so
// the same files give the same archive.
func writeZip(out string, files map[string][]byte) error {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, name := range names {
		h := &zip.FileHeader{Name: path.Join(Root, name), Method: zip.Deflate, Modified: stamp}
		h.SetMode(0o644)
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err := w.Write(files[name]); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	tmp := out + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, out)
}
