package deploy

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestInstallPublishesAndCleansUp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux installer requires a Unix shell")
	}
	for _, mode := range []string{"trusted", "secure"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "target")
			payload := filepath.Join(root, "payload")
			if err := os.Mkdir(payload, 0700); err != nil {
				t.Fatal(err)
			}
			entries := map[string][]byte{
				"maiservice":          []byte("binary"),
				"ecosystem.config.js": []byte("ecosystem"),
				"install.sh":          []byte(installScript(Options{Mode: mode, InstallDir: target})),
				"verify.sh":           []byte(verifyScript),
			}
			if mode == "trusted" {
				entries["config/config.json"] = []byte(`{"value":"original"}`)
			} else {
				entries["config/config.json.age"] = []byte("encrypted")
			}
			keys := make([]string, 0, len(entries))
			for name := range entries {
				keys = append(keys, name)
			}
			sort.Strings(keys)
			var manifest strings.Builder
			for _, name := range keys {
				path := filepath.Join(payload, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				fileMode := os.FileMode(0600)
				if name == "maiservice" || strings.HasSuffix(name, ".sh") {
					fileMode = 0700
				}
				if err := os.WriteFile(path, entries[name], fileMode); err != nil {
					t.Fatal(err)
				}
				sum := sha256.Sum256(entries[name])
				fmt.Fprintf(&manifest, "%x  %s\n", sum, name)
			}
			if err := os.WriteFile(filepath.Join(payload, "SHA256SUMS"), []byte(manifest.String()), 0600); err != nil {
				t.Fatal(err)
			}
			command := func() *exec.Cmd {
				c := exec.Command("sh", filepath.Join(payload, "install.sh"))
				c.Env = os.Environ()
				return c
			}
			if mode == "secure" {
				fakebin := filepath.Join(root, "fakebin")
				if err := os.Mkdir(fakebin, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(fakebin, "age"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
					t.Fatal(err)
				}
				c := command()
				c.Env = append(c.Env, "PATH="+fakebin+string(os.PathListSeparator)+os.Getenv("PATH"), "AGE_IDENTITY=unused")
				if output, err := c.CombinedOutput(); err == nil {
					t.Fatalf("failed decryption succeeded: %s", output)
				}
				if _, err := os.Stat(target); !os.IsNotExist(err) {
					t.Fatal("failed installation left a target")
				}
				stages, err := filepath.Glob(target + ".tmp.*")
				if err != nil || len(stages) != 0 {
					t.Fatalf("failed installation left stages: %v (%v)", stages, err)
				}
				return
			}
			if output, err := command().CombinedOutput(); err != nil {
				t.Fatalf("installation failed: %v: %s", err, output)
			}
			config := filepath.Join(target, "config", "config.json")
			data, err := os.ReadFile(config)
			if err != nil || string(data) != string(entries["config/config.json"]) {
				t.Fatal("installed config differs")
			}
			st, err := os.Stat(filepath.Join(target, "maiservice"))
			if err != nil || st.Mode()&0111 == 0 {
				t.Fatal("installed binary is not executable")
			}
			if err := os.WriteFile(config, []byte("preserve"), 0600); err != nil {
				t.Fatal(err)
			}
			if output, err := command().CombinedOutput(); err == nil {
				t.Fatalf("existing target was overwritten: %s", output)
			}
			data, err = os.ReadFile(config)
			if err != nil || string(data) != "preserve" {
				t.Fatal("existing config changed")
			}
		})
	}
}
