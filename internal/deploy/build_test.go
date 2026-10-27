package deploy

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"filippo.io/age"
)

func TestProfiles(t *testing.T) {
	o := Options{Arch: "amd64", ConfigPath: "config.json", InstallDir: "/opt/maiservice", ServiceName: "maiservice", Port: 3081, MemoryMB: 650, GoMemoryMB: 400, Mode: "trusted"}
	if err := validate(o); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Options){func(x *Options) { x.Arch = "386" }, func(x *Options) { x.InstallDir = "/opt/a;evil" }, func(x *Options) { x.ServiceName = "a;evil" }, func(x *Options) { x.Mode = "" }, func(x *Options) { x.GoMemoryMB = 1000 }} {
		bad := o
		change(&bad)
		if validate(bad) == nil {
			t.Fatal("accepted invalid profile")
		}
	}
}
func TestSecurePackage(t *testing.T) {
	if testing.Short() {
		t.Skip("builds service")
	}
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	dir, err := os.MkdirTemp(root, ".package-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	cfg := []byte(`{"db_password":"test-secret","db_enabled":true,"sap_enabled":true}`)
	if err = os.WriteFile(filepath.Join(dir, "config.json"), cfg, 0600); err != nil {
		t.Fatal(err)
	}
	rel, _ := filepath.Rel(root, filepath.Join(dir, "config.json"))
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	o := Options{Arch: "amd64", ConfigPath: rel, InstallDir: "/opt/maiservice", ServiceName: "maiservice", Port: 3081, MemoryMB: 650, GoMemoryMB: 400, Mode: "secure", SigningKey: base64.StdEncoding.EncodeToString(priv), Recipient: identity.Recipient().String()}
	r, err := Build(context.Background(), root, o)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(filepath.Join(root, r.Path)))
	if r.SigningPublicKey != base64.StdEncoding.EncodeToString(pub) {
		t.Fatal("wrong public key")
	}
	z, err := zip.OpenReader(filepath.Join(root, r.Path))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	entries := map[string][]byte{}
	for _, f := range z.File {
		r, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(r)
		r.Close()
		if e != nil {
			t.Fatal(e)
		}
		entries[f.Name] = b
		if bytes.Contains(b, cfg) || bytes.Contains(b, priv) {
			t.Fatalf("secret leaked in %s", f.Name)
		}
	}
	var release struct {
		OS        string `json:"os"`
		Arch      string `json:"arch"`
		GoVersion string `json:"goVersion"`
	}
	if err := json.Unmarshal(entries["mai/release.json"], &release); err != nil || release.OS != "linux" || release.Arch != "amd64" || !strings.HasPrefix(release.GoVersion, "go") {
		t.Fatalf("invalid release metadata: %+v (%v)", release, err)
	}
	var bom struct {
		BOMFormat   string `json:"bomFormat"`
		SpecVersion string `json:"specVersion"`
		Version     int    `json:"version"`
		Components  []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"components"`
	}
	if err := json.Unmarshal(entries["mai/sbom.cdx.json"], &bom); err != nil || bom.BOMFormat != "CycloneDX" || bom.SpecVersion != "1.5" || bom.Version != 1 || len(bom.Components) == 0 {
		t.Fatalf("invalid SBOM: %+v (%v)", bom, err)
	}
	found := false
	for _, c := range bom.Components {
		if c.Name == "github.com/xuri/excelize/v2" && c.Version != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected compiled Go dependency absent from SBOM")
	}
	for _, name := range []string{"release.json", "sbom.cdx.json"} {
		sum := sha256.Sum256(entries["mai/"+name])
		if !strings.Contains(string(entries["mai/SHA256SUMS"]), fmt.Sprintf("%x  %s\n", sum, name)) {
			t.Fatalf("%s missing from signed manifest", name)
		}
	}
	instructions := string(entries["mai/DEPLOY.md"])
	verifierAt := strings.Index(instructions, "python3 verify-mai-package.py --trusted-key")
	extractionAt := strings.Index(instructions, "unzip mai.zip")
	if verifierAt < 0 || extractionAt < 0 || verifierAt >= extractionAt || strings.Contains(instructions, "unzip -p") {
		t.Fatal("deployment instructions must verify the archive before extraction")
	}
	trustedInstructions := docs(Options{Mode: "trusted"}, nil)
	if v, e := strings.Index(trustedInstructions, "python3 verify-mai-package.py --allow-unsigned"), strings.Index(trustedInstructions, "unzip mai.zip"); v < 0 || e < 0 || v >= e {
		t.Fatal("trusted package must be verified before extraction")
	}
	ecosystem := string(entries["mai/ecosystem.config.js"])
	for _, setting := range []string{`script: "/opt/maiservice/maiservice"`, `cwd: "/opt/maiservice"`, `interpreter: 'none'`, `exec_mode: 'fork'`, `instances: 1`, `autorestart: true`, `min_uptime: '10s'`, `max_restarts: 10`, `exp_backoff_restart_delay: 100`, `kill_timeout: 15000`} {
		if !strings.Contains(ecosystem, setting) {
			t.Fatalf("PM2 configuration missing %s", setting)
		}
	}
	decrypted, err := age.Decrypt(bytes.NewReader(entries["mai/config/config.json.age"]), identity)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(decrypted)
	if err != nil || !bytes.Equal(actual, cfg) {
		t.Fatal("config decryption failed")
	}
	if runtime.GOOS != "windows" {
		extract := t.TempDir()
		for name, b := range entries {
			path := filepath.Join(extract, name)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, b, 0600); err != nil {
				t.Fatal(err)
			}
		}
		verifier := filepath.Join(extract, "mai", "verify.sh")
		if output, err := exec.Command("sh", verifier).CombinedOutput(); err != nil {
			t.Fatalf("verifier failed: %s", output)
		}
		if err := os.MkdirAll(filepath.Join(extract, "mai/config/workflows"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(extract, "mai/config/workflows/extra.yaml"), []byte("unexpected"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := exec.Command("sh", verifier).Run(); err == nil {
			t.Fatal("verifier accepted an unlisted workflow")
		}
	}
	manifest := entries["mai/SHA256SUMS"]
	if !ed25519.Verify(pub, manifest, entries["mai/SHA256SUMS.sig"]) {
		t.Fatal("signature verification failed")
	}
	manifest[0] ^= 1
	if ed25519.Verify(pub, manifest, entries["mai/SHA256SUMS.sig"]) {
		t.Fatal("tampered manifest accepted")
	}
	if !strings.Contains(r.Validation, "ELF verified") {
		t.Fatal(r.Validation)
	}
	// Cross-architecture packages must explicitly report that they were not run.
	o.Arch = "arm64"
	o.Mode = "trusted"
	o.SigningKey = ""
	o.Recipient = ""
	arm, err := Build(context.Background(), root, o)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(filepath.Join(root, arm.Path)))
	if runtime.GOARCH != "arm64" && !strings.Contains(arm.Validation, "runtime not tested") {
		t.Fatal(arm.Validation)
	}

}
func TestScopedRejectsEscape(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "config.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(external, "config.json"), filepath.Join(root, "config.json")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation is unavailable")
		}
		t.Fatal(err)
	}
	if _, err := readInput(root, "config.json", 1024); err == nil {
		t.Fatal("accepted escaping symlink")
	}
}

func TestBuildEnvironmentUsesVerifiedModuleDownloads(t *testing.T) {
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOTOOLCHAIN", "auto")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	windowsVars := []string{"TEMP", "TMP", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "SYSTEMROOT", "SystemDrive", "COMSPEC", "PATHEXT"}
	for _, key := range windowsVars {
		t.Setenv(key, "test-value-"+key)
	}
	env := map[string]string{}
	for _, pair := range safeEnv("arm64") {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			t.Fatalf("malformed environment value %q", pair)
		}
		if _, duplicate := env[key]; duplicate {
			t.Fatalf("duplicate environment key %s", key)
		}
		env[key] = value
	}
	for _, key := range windowsVars {
		if env[key] != "test-value-"+key {
			t.Fatalf("%s was not preserved", key)
		}
	}
	for key, value := range map[string]string{"GOOS": "linux", "GOARCH": "arm64", "CGO_ENABLED": "0", "GOFLAGS": "", "GOTOOLCHAIN": "local", "GOWORK": "off", "GOPROXY": "https://proxy.golang.org", "GOSUMDB": "sum.golang.org"} {
		if env[key] != value {
			t.Fatalf("%s = %q, want %q", key, env[key], value)
		}
	}
}
