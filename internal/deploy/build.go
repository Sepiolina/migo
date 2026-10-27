// Package deploy builds scoped Linux deployment archives.
package deploy

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"debug/buildinfo"
	"debug/elf"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"filippo.io/age"
	"miservice/internal/workflow"
)

type Options struct {
	Arch        string   `json:"arch"`
	ConfigPath  string   `json:"configPath"`
	Workflows   []string `json:"workflows"`
	InstallDir  string   `json:"installDir"`
	ServiceName string   `json:"serviceName"`
	Port        int      `json:"port"`
	MemoryMB    int      `json:"memoryMB"`
	GoMemoryMB  int      `json:"goMemoryMB"`
	Mode        string   `json:"mode"`
	SigningKey  string   `json:"signingKey,omitempty"`
	Recipient   string   `json:"recipient,omitempty"`
}
type Result struct {
	Path             string   `json:"path"`
	SHA256           string   `json:"sha256"`
	Arch             string   `json:"arch"`
	Mode             string   `json:"mode"`
	Files            []string `json:"files"`
	Validation       string   `json:"validation"`
	SigningPublicKey string   `json:"signingPublicKey,omitempty"`
}

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
var dirRE = regexp.MustCompile(`^/opt/[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
var workflowRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}\.yaml$`)

func scoped(root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", errors.New("input paths must be relative to the workspace")
	}
	p, err := filepath.EvalSymlinks(filepath.Join(root, rel))
	if err != nil {
		return "", errors.New("input file does not exist")
	}
	p, err = filepath.Abs(p)
	if err != nil {
		return "", err
	}
	r, err := filepath.Rel(root, p)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", errors.New("input path escapes workspace")
	}
	return p, nil
}
func readInput(root, rel string, limit int64) ([]byte, error) {
	p, err := scoped(root, rel)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, errors.New("cannot read input file")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > limit {
		return nil, errors.New("input must be a regular file within the size limit")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, errors.New("cannot read bounded input file")
	}
	return b, nil
}
func validate(o Options) error {
	if o.Arch != "amd64" && o.Arch != "arm64" {
		return errors.New("architecture must be amd64 or arm64")
	}
	if o.Mode != "trusted" && o.Mode != "secure" {
		return errors.New("mode must be trusted or secure")
	}
	if !dirRE.MatchString(o.InstallDir) || !nameRE.MatchString(o.ServiceName) {
		return errors.New("invalid install directory or service name")
	}
	if o.Port < 1 || o.Port > 65535 || o.MemoryMB < 64 || o.MemoryMB > 65536 || o.GoMemoryMB < 32 || o.GoMemoryMB > o.MemoryMB {
		return errors.New("invalid port or memory limits")
	}
	if len(o.Workflows) > 100 {
		return errors.New("at most 100 workflows are allowed")
	}
	if filepath.Base(o.ConfigPath) != "config.json" {
		return errors.New("configPath must identify config.json")
	}
	return nil
}
func Build(ctx context.Context, workspace string, o Options) (Result, error) {
	var result Result
	if err := validate(o); err != nil {
		return result, err
	}
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return result, errors.New("invalid workspace")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return result, err
	}
	if _, err = scoped(root, "go.mod"); err != nil {
		return result, errors.New("workspace must contain go.mod")
	}
	if _, err = scoped(root, "cmd/maiservice/main.go"); err != nil {
		return result, errors.New("workspace must contain cmd/maiservice")
	}
	config, err := readInput(root, o.ConfigPath, 1<<20)
	if err != nil {
		return result, err
	}
	var obj map[string]any
	if json.Unmarshal(bytes.TrimPrefix(config, []byte("\xef\xbb\xbf")), &obj) != nil || obj == nil {
		return result, errors.New("config.json must contain a JSON object")
	}
	entries := map[string][]byte{}
	names := map[string]bool{}
	for _, p := range o.Workflows {
		name := filepath.Base(p)
		if !workflowRE.MatchString(name) || names[name] {
			return result, errors.New("workflow names must be unique lowercase YAML filenames")
		}
		names[name] = true
		b, e := readInput(root, p, 1<<20)
		if e != nil {
			return result, e
		}
		if _, e = workflow.Parse(b); e != nil {
			return result, fmt.Errorf("invalid workflow %s", name)
		}
		entries["mai/config/workflows/"+name] = b
	}
	var private ed25519.PrivateKey
	var pubPEM []byte
	if o.Mode == "secure" {
		key, e := base64.StdEncoding.DecodeString(o.SigningKey)
		if e != nil || len(key) != ed25519.PrivateKeySize {
			return result, errors.New("secure mode requires an Ed25519 private key")
		}
		private = ed25519.PrivateKey(key)
		if !bytes.Equal(private, ed25519.NewKeyFromSeed(private[:32])) {
			return result, errors.New("invalid Ed25519 private key")
		}
		recipient, e := age.ParseX25519Recipient(o.Recipient)
		if e != nil {
			return result, errors.New("secure mode requires a valid age recipient")
		}
		var enc bytes.Buffer
		w, e := age.Encrypt(&enc, recipient)
		if e != nil {
			return result, errors.New("could not encrypt config")
		}
		if _, e = w.Write(config); e != nil {
			return result, errors.New("could not encrypt config")
		}
		if e = w.Close(); e != nil {
			return result, errors.New("could not encrypt config")
		}
		entries["mai/config/config.json.age"] = enc.Bytes()
		der, _ := x509.MarshalPKIXPublicKey(private.Public())
		pubPEM = pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
		result.SigningPublicKey = base64.StdEncoding.EncodeToString(private.Public().(ed25519.PublicKey))
	} else {
		entries["mai/config/config.json"] = config
	}
	tmp, err := os.MkdirTemp("", "mai-build-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(tmp)
	binary := filepath.Join(tmp, "maiservice")
	buildCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(buildCtx, "go", "build", "-mod=readonly", "-trimpath", "-o", binary, "./cmd/maiservice")
	cmd.Dir = root
	configureBuildProcess(cmd)
	cmd.Env = safeEnv(o.Arch)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err = cmd.Run(); err != nil {
		if buildCtx.Err() != nil {
			return result, errors.New("Go build timed out or was cancelled")
		}
		return result, errors.New("Go build failed; check the workspace and Go dependencies")
	}
	ef, err := elf.Open(binary)
	if err != nil {
		return result, errors.New("build did not produce a Linux ELF executable")
	}
	expected := elf.EM_X86_64
	if o.Arch == "arm64" {
		expected = elf.EM_AARCH64
	}
	valid := ef.Machine == expected && ef.Class == elf.ELFCLASS64
	ef.Close()
	if !valid {
		return result, errors.New("built ELF architecture does not match the selected target")
	}
	exe, err := os.ReadFile(binary)
	if err != nil {
		return result, err
	}
	entries["mai/maiservice"] = exe
	release, sbom, err := metadata(binary, o.Arch)
	if err != nil {
		return result, err
	}
	entries["mai/release.json"] = release
	entries["mai/sbom.cdx.json"] = sbom
	result.Validation = "ELF verified; runtime not tested for this target"
	if runtime.GOOS == "linux" && runtime.GOARCH == o.Arch && supportsSmoke(root) {
		if err = smoke(ctx, binary, tmp, entries); err != nil {
			return result, err
		}
		result.Validation = "ELF verified; local health check passed with database and SAP disabled"
	}
	entries["mai/ecosystem.config.js"] = []byte(fmt.Sprintf("module.exports = { apps: [{ name: %q, script: %q, cwd: %q, interpreter: 'none', exec_mode: 'fork', instances: 1, autorestart: true, min_uptime: '10s', max_restarts: 10, exp_backoff_restart_delay: 100, kill_timeout: 15000, args: ['-config', %q], env: { PORT: %q, GOMEMLIMIT: %q }, max_memory_restart: %q }] };\n", o.ServiceName, o.InstallDir+"/maiservice", o.InstallDir, o.InstallDir+"/config/config.json", fmt.Sprint(o.Port), fmt.Sprintf("%dMiB", o.GoMemoryMB), fmt.Sprintf("%dM", o.MemoryMB)))
	entries["mai/install.sh"] = []byte(installScript(o))
	entries["mai/verify.sh"] = []byte(verifyScript)
	entries["mai/DEPLOY.md"] = []byte(docs(o, pubPEM))
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var manifest strings.Builder
	for _, k := range keys {
		h := sha256.Sum256(entries[k])
		fmt.Fprintf(&manifest, "%x  %s\n", h, strings.TrimPrefix(k, "mai/"))
	}
	entries["mai/SHA256SUMS"] = []byte(manifest.String())
	if o.Mode == "secure" {
		entries["mai/SHA256SUMS.sig"] = ed25519.Sign(private, []byte(manifest.String()))
	}
	// Confirm the exact bytes used for the manifest before writing the archive.
	for _, k := range keys {
		h := sha256.Sum256(entries[k])
		if !strings.Contains(manifest.String(), fmt.Sprintf("%x  %s\n", h, strings.TrimPrefix(k, "mai/"))) {
			return result, errors.New("package integrity check failed")
		}
	}
	parent := root
	for _, part := range []string{".studio", "packages"} {
		next := filepath.Join(parent, part)
		if e := os.Mkdir(next, 0700); e != nil && !os.IsExist(e) {
			return result, e
		}
		parent, err = scoped(root, strings.TrimPrefix(next, root+string(filepath.Separator)))
		if err != nil {
			return result, err
		}
	}
	dir, err := os.MkdirTemp(parent, "package-")
	if err != nil {
		return result, err
	}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(dir)
		}
	}()
	out := filepath.Join(dir, "mai.zip")
	f, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return result, err
	}
	z := zip.NewWriter(f)
	keys = keys[:0]
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h := &zip.FileHeader{Name: k, Method: zip.Deflate}
		mode := os.FileMode(0600)
		if k == "mai/maiservice" || strings.HasSuffix(k, ".sh") {
			mode = 0700
		}
		h.SetMode(mode)
		w, e := z.CreateHeader(h)
		if e == nil {
			_, e = w.Write(entries[k])
		}
		if e != nil {
			z.Close()
			f.Close()
			return result, e
		}
	}
	if err = z.Close(); err != nil {
		f.Close()
		return result, err
	}
	if err = f.Close(); err != nil {
		return result, err
	}
	if err = verifyArchive(out, entries); err != nil {
		return result, err
	}
	archive, err := os.ReadFile(out)
	if err != nil {
		return result, err
	}
	sum := sha256.Sum256(archive)
	rel, _ := filepath.Rel(root, out)
	result.Path = filepath.ToSlash(rel)
	result.SHA256 = hex.EncodeToString(sum[:])
	result.Arch = o.Arch
	result.Mode = o.Mode
	result.Files = keys
	success = true
	return result, nil
}
func safeEnv(arch string) []string {
	env := []string{"GOOS=linux", "GOARCH=" + arch, "CGO_ENABLED=0", "GOFLAGS=", "GOENV=off", "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=https://proxy.golang.org", "GOSUMDB=sum.golang.org"}
	for _, k := range []string{"PATH", "HOME", "GOPATH", "GOCACHE", "GOMODCACHE", "GOROOT", "TMPDIR", "TEMP", "TMP", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "SYSTEMROOT", "SystemDrive", "COMSPEC", "PATHEXT"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}
func verifyArchive(path string, expected map[string][]byte) error {
	z, e := zip.OpenReader(path)
	if e != nil {
		return errors.New("cannot verify package")
	}
	defer z.Close()
	if len(z.File) != len(expected) {
		return errors.New("package integrity check failed")
	}
	seen := map[string]bool{}
	for _, f := range z.File {
		want, ok := expected[f.Name]
		if !ok || seen[f.Name] {
			return errors.New("package integrity check failed")
		}
		seen[f.Name] = true
		r, e := f.Open()
		if e != nil {
			return errors.New("package integrity check failed")
		}
		b, e := io.ReadAll(io.LimitReader(r, int64(len(want))+1))
		r.Close()
		if e != nil || !bytes.Equal(b, want) {
			return errors.New("package integrity check failed")
		}
	}
	return nil
}
func smoke(ctx context.Context, binary, tmp string, entries map[string][]byte) error {
	dir := filepath.Join(tmp, "smoke")
	if err := os.MkdirAll(filepath.Join(dir, "workflows"), 0700); err != nil {
		return err
	}
	for name, b := range entries {
		if strings.HasPrefix(name, "mai/config/workflows/") {
			if err := os.WriteFile(filepath.Join(dir, "workflows", filepath.Base(name)), b, 0600); err != nil {
				return err
			}
		}
	}
	b := []byte(`{"db_enabled":false,"sap_enabled":false}`)
	config := filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, b, 0600); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listenerFile, err := listener.(*net.TCPListener).File()
	if err != nil {
		listener.Close()
		return errors.New("local health check could not reserve a port")
	}
	defer listenerFile.Close()
	defer listener.Close()
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, binary, "-config", config)
	cmd.Env = []string{fmt.Sprintf("PORT=%d", port), "PATH=" + os.Getenv("PATH"), "MAISERVICE_LISTEN_FD=3"}
	cmd.ExtraFiles = []*os.File{listenerFile}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		return errors.New("local health check could not start")
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	client := http.Client{Timeout: 500 * time.Millisecond}
	for cctx.Err() == nil {
		req, _ := http.NewRequestWithContext(cctx, "GET", fmt.Sprintf("http://127.0.0.1:%d/api/health", port), nil)
		resp, e := client.Do(req)
		if e == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		select {
		case <-cctx.Done():
		case <-time.After(100 * time.Millisecond):
		}
	}
	return errors.New("local health check failed")
}
func installScript(o Options) string {
	decrypt := "cp config/config.json \"$stage/config/config.json\""
	if o.Mode == "secure" {
		decrypt = "[ \"${AGE_IDENTITY:-}\" ] || { echo 'Set AGE_IDENTITY to the age identity file' >&2; exit 1; }\nage -d -i \"$AGE_IDENTITY\" -o \"$stage/config/config.json\" config/config.json.age"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
cd "$(dirname "$0")"
./verify.sh
target=%q
umask 077
[ ! -e "$target" ] && [ ! -L "$target" ] || { echo 'Target already exists; move it aside first' >&2; exit 1; }
stage=$(mktemp -d "${target}.tmp.XXXXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
mkdir -p "$stage/config/workflows"
cp maiservice ecosystem.config.js "$stage/"
%s
for f in config/workflows/*.yaml; do [ ! -f "$f" ] || cp "$f" "$stage/config/workflows/"; done
mv -T -n "$stage" "$target"
[ ! -e "$stage" ] || { echo 'Target already exists' >&2; exit 1; }
trap - EXIT HUP INT TERM
printf 'Installed. Start with: pm2 start %%s/ecosystem.config.js\n' "$target"
`, o.InstallDir, decrypt)
}
func docs(o Options, pub []byte) string {
	verify := `This trusted package contains plaintext configuration and has no authenticity signature. Transfer and store it only over trusted channels. Verify the ZIP with the independently obtained verifier before extraction:

    python3 verify-mai-package.py --allow-unsigned mai.zip

`
	if o.Mode == "secure" {
		verify = fmt.Sprintf(`Obtain the signing public key independently from Studio through a trusted channel. The archive claims this base64 raw Ed25519 public key: %s. A key found only in the archive is not a trust anchor. Verify the ZIP with the independently obtained verifier and the independently retained key before extraction:

    python3 verify-mai-package.py --trusted-key 'BASE64_PUBLIC_KEY_FROM_STUDIO' mai.zip

Protect the age identity on the target. Install age and set AGE_IDENTITY to the identity file before installation.

`, base64.StdEncoding.EncodeToString(mustPublic(pub)))
	}
	return "# Linux deployment\n\n" + `Obtain [scripts/verify-mai-package.py](https://github.com/Sepiolina/migo/blob/main/scripts/verify-mai-package.py) from a trusted repository checkout, independently of this archive. The verifier requires Python 3 and OpenSSL. Do not use a verifier or public key supplied only inside the ZIP to establish trust.

` + verify + `After the independent verifier succeeds, extract into a new empty directory:

    unzip mai.zip
    cd mai
    ./verify.sh
    ./install.sh

The bundled verify.sh provides an additional check after extraction; it does not establish initial trust. Installation requires a new target directory; move an existing installation aside first. Install Node.js and PM2 on the target, then start the printed PM2 command. release.json records build metadata. sbom.cdx.json lists Go modules in the compiled binary; it is not a vulnerability audit. The service reads configuration and workflow overrides from its config directory. Built-in workflows are embedded in the service; selected workflow files override them or add endpoints rather than restricting the built-in endpoints. The configured memory restart is a PM2 threshold; GOMEMLIMIT is a Go runtime soft limit. Back up existing configuration and workflows before installing.
`
}
func mustPublic(p []byte) []byte {
	block, _ := pem.Decode(p)
	if block == nil {
		return nil
	}
	key, e := x509.ParsePKIXPublicKey(block.Bytes)
	if e != nil {
		return nil
	}
	return key.(ed25519.PublicKey)
}

const verifyScript = `#!/bin/sh
set -eu
cd "$(dirname "$0")"
python3 - <<'PYVERIFY'
import hashlib, os, pathlib, sys
root = pathlib.Path('.')
expected = {}
for line in pathlib.Path('SHA256SUMS').read_text(encoding='utf-8').splitlines():
    digest, sep, name = line.partition('  ')
    if not sep or len(digest) != 64 or any(c not in '0123456789abcdef' for c in digest) or not name or name.startswith('/') or any(p in ('', '.', '..') for p in name.split('/')) or name in expected:
        sys.exit('invalid manifest')
    expected[name] = digest
actual = set()
for parent, dirs, files in os.walk(root, followlinks=False):
    for name in dirs + files:
        path = pathlib.Path(parent) / name
        if path.is_symlink(): sys.exit('symlink in package')
    for name in files:
        path = pathlib.Path(parent) / name
        rel = path.as_posix()
        if not path.is_file(): sys.exit('non-regular file in package')
        if rel not in ('SHA256SUMS', 'SHA256SUMS.sig'):
            actual.add(rel)
            if rel not in expected or hashlib.sha256(path.read_bytes()).hexdigest() != expected[rel]:
                sys.exit('package integrity check failed')
if actual != set(expected): sys.exit('package files differ from manifest')
print('Package checksums and file list verified')
PYVERIFY
`

// Only a service source with the reviewed inherited-listener implementation can
// safely be launched for validation. Older checkouts may bind all interfaces.
func supportsSmoke(root string) bool {
	b, err := readInput(root, "internal/app/server.go", 1<<20)
	if err != nil {
		return false
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]) == "a2a0c148e11a775c3036aba6ad5529504eeef821caacfbaeee5de7148b114f72"
}

// metadata records the binary's build information, not a vulnerability audit.
func metadata(binary, arch string) ([]byte, []byte, error) {
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		return nil, nil, errors.New("built executable lacks Go build information")
	}
	release := struct {
		OS        string `json:"os"`
		Arch      string `json:"arch"`
		GoVersion string `json:"goVersion"`
		Revision  string `json:"vcsRevision,omitempty"`
		Modified  *bool  `json:"vcsModified,omitempty"`
	}{OS: "linux", Arch: arch, GoVersion: info.GoVersion}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`).MatchString(setting.Value) {
				release.Revision = setting.Value
			}
		case "vcs.modified":
			if setting.Value == "true" || setting.Value == "false" {
				v := setting.Value == "true"
				release.Modified = &v
			}
		}
	}
	type property struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	type component struct {
		Type       string     `json:"type"`
		Name       string     `json:"name"`
		Version    string     `json:"version,omitempty"`
		Properties []property `json:"properties,omitempty"`
	}
	components := make([]component, 0, len(info.Deps))
	for _, module := range info.Deps {
		if module == nil || module.Path == "" || strings.HasPrefix(module.Path, ".") || strings.HasPrefix(module.Path, "/") || strings.Contains(module.Path, `\`) {
			continue
		}
		c := component{Type: "library", Name: module.Path, Version: module.Version}
		if module.Replace != nil {
			replacement := module.Replace
			if replacement.Version != "" && replacement.Path != "" && !strings.HasPrefix(replacement.Path, ".") && !strings.HasPrefix(replacement.Path, "/") && !strings.Contains(replacement.Path, `\`) {
				c.Properties = []property{{Name: "go:replacement:name", Value: replacement.Path}, {Name: "go:replacement:version", Value: replacement.Version}}
			} else {
				c.Properties = []property{{Name: "go:replacement", Value: "local replacement (path omitted)"}}
			}
		}
		components = append(components, c)
	}
	sort.Slice(components, func(i, j int) bool { return components[i].Name < components[j].Name })
	bom := struct {
		BOMFormat   string      `json:"bomFormat"`
		SpecVersion string      `json:"specVersion"`
		Version     int         `json:"version"`
		Components  []component `json:"components"`
	}{"CycloneDX", "1.5", 1, components}
	r, err := json.MarshalIndent(release, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	b, err := json.MarshalIndent(bom, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return append(r, '\n'), append(b, '\n'), nil
}
