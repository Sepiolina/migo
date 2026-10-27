package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}
func request(t *testing.T, command string, p Params) Response {
	t.Helper()
	r := Handle(context.Background(), Request{Version: 1, ID: "test", Command: command, Params: p})
	return r
}
func TestValidateAndInspect(t *testing.T) {
	root := fixtureRoot(t)
	yaml, err := os.ReadFile(filepath.Join(root, "examples/parts-summary.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	r := request(t, "validate", Params{YAML: string(yaml)})
	if !r.OK {
		t.Fatal(r.Error)
	}
	v := r.Result.(Validation)
	if len(v.Inputs) != 1 || v.Inputs[0].Name != "parts" {
		t.Fatalf("wrong inputs: %+v", v)
	}
	r = request(t, "inspectWorkbook", Params{Workspace: root, Path: "examples/parts.xlsx", Limit: 1})
	if !r.OK {
		t.Fatal(r.Error)
	}
	p := r.Result.(Preview)
	if len(p.Rows) != 1 || p.TotalRows < 2 || !p.Truncated {
		t.Fatalf("incorrect pagination: %+v", p)
	}
}
func TestRunUsesEngineAndProducesArtifact(t *testing.T) {
	root := fixtureRoot(t)
	workspace := t.TempDir()
	data, err := os.ReadFile(filepath.Join(root, "examples/parts.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(workspace, "parts.xlsx"), data, 0600); err != nil {
		t.Fatal(err)
	}
	yaml, err := os.ReadFile(filepath.Join(root, "examples/parts-summary.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	r := request(t, "run", Params{Workspace: workspace, YAML: string(yaml), Inputs: map[string]string{"parts": "parts.xlsx"}})
	if !r.OK {
		t.Fatal(r.Error)
	}
	result := r.Result.(map[string]any)
	if len(result["sha256"].(string)) != 64 {
		t.Fatal("missing hash")
	}
	if _, err := os.Stat(filepath.Join(workspace, result["path"].(string))); err != nil {
		t.Fatal(err)
	}
	preview := result["preview"].(Preview)
	if preview.TotalRows < 2 {
		t.Fatal("empty generated workbook")
	}
}
func TestPathsStayInWorkspace(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "file"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := localPath(root, filepath.Join(outside, "file")); err == nil {
		t.Fatal("absolute escape accepted")
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skip(err)
	}
	if _, err := localPath(root, "escape/file"); err == nil {
		t.Fatal("symlink escape accepted")
	}
}
func TestProtocolErrorsAndFrameLimit(t *testing.T) {
	var out bytes.Buffer
	if err := Serve(context.Background(), strings.NewReader("bad\n{\"version\":2,\"id\":\"a\",\"command\":\"capabilities\"}\n"), &out); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(&out)
	for _, code := range []string{"invalid_request", "unsupported_version"} {
		var r Response
		if err := dec.Decode(&r); err != nil {
			t.Fatal(err)
		}
		if r.OK || r.Error.Code != code {
			t.Fatalf("incorrect failure: %+v", r)
		}
	}
	if err := Serve(context.Background(), strings.NewReader(strings.Repeat("x", MaxFrameBytes+1)), &out); err == nil {
		t.Fatal("oversized frame accepted")
	}
}
func TestOfflineTransport(t *testing.T) {
	if _, err := (offlineTransport{}).RoundTrip(nil); err == nil {
		t.Fatal("HTTP available")
	}
}
