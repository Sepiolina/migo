package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The kit is built twice to the same bytes, its manifest matches its files,
// and, unpacked on its own, it vets, passes the engine's tests and passes
// the conformance suite. That is what an agent is told to check.
func TestKitStandsAlone(t *testing.T) {
	dir := t.TempDir()
	first, err := Build("../..", filepath.Join(dir, "a.zip"), "test")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build("../..", filepath.Join(dir, "b.zip"), "test")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(first)
	b, _ := os.ReadFile(second)
	if !bytes.Equal(a, b) {
		t.Fatal("two builds of the same tree differ")
	}

	zr, err := zip.NewReader(bytes.NewReader(a), int64(len(a)))
	if err != nil {
		t.Fatal(err)
	}
	unpacked := filepath.Join(dir, "unpacked")
	files := map[string][]byte{}
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, Root+"/") || strings.Contains(f.Name, "..") {
			t.Fatalf("unexpected entry %q", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimPrefix(f.Name, Root+"/")
		files[name] = data
		target := filepath.Join(unpacked, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, need := range []string{"AGENTS.md", "CLAUDE.md", "README.md", "MANIFEST.json", "engine/go.mod", "engine/cmd/migo/main.go", "conformance/bom.test.yaml", "docs/WORKFLOWS.md"} {
		if files[need] == nil {
			t.Errorf("the kit has no %s", need)
		}
	}
	var m Manifest
	if err := json.Unmarshal(files["MANIFEST.json"], &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Files) != len(files)-1 || m.Conformance == 0 || m.Version != "test" {
		t.Fatalf("manifest: %d files for %d, %d suites, version %q", len(m.Files), len(files)-1, m.Conformance, m.Version)
	}
	for name, sum := range m.Files {
		got := sha256.Sum256(files[name])
		if hex.EncodeToString(got[:]) != sum {
			t.Errorf("%s: manifest digest does not match", name)
		}
	}
	for name := range files {
		if strings.HasPrefix(name, "apps/") || strings.HasPrefix(name, "internal/") || strings.HasPrefix(name, "cmd/") {
			t.Errorf("%s is service code; the kit holds the engine only", name)
		}
	}

	if testing.Short() {
		t.Skip("skipping the unpacked kit's checks in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not on PATH")
	}
	engine := filepath.Join(unpacked, Root, "engine")
	for _, args := range [][]string{
		{"vet", "./..."},
		{"test", "./..."},
		{"run", "./cmd/migo", "test", "../conformance"},
	} {
		cmd := exec.Command("go", args...)
		cmd.Dir = engine
		// The kit must build on its own: no workspace file, no access to the repository.
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=readonly")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s in the unpacked kit: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}
