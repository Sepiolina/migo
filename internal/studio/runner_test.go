package studio

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sepiolina/migo/engine/wftest"
	"github.com/Sepiolina/migo/engine/workflow"
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
	t.Run("defer preview until the workbook is opened", func(t *testing.T) {
		params := Params{Workspace: workspace, YAML: string(yaml), Inputs: map[string]string{"parts": "parts.xlsx"}, OmitPreview: true}
		saved, err := runWithInspect(context.Background(), params, func(string, Params) (Preview, error) {
			t.Fatal("unexpected inline preview")
			return Preview{}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		artifact := saved.(map[string]any)
		if artifact["preview"] != nil || artifact["sha256"] == nil {
			t.Fatalf("unexpected deferred run result: %+v", artifact)
		}
		if _, err := os.Stat(filepath.Join(workspace, artifact["path"].(string))); err != nil {
			t.Fatal(err)
		}
	})
}
func TestRunPreservesArtifactWhenPreviewExceedsLimit(t *testing.T) {
	root := fixtureRoot(t)
	workspace := t.TempDir()
	data, err := os.ReadFile(filepath.Join(root, "examples/parts.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "parts.xlsx"), data, 0600); err != nil {
		t.Fatal(err)
	}
	yaml, err := os.ReadFile(filepath.Join(root, "examples/parts-summary.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the size-limit failure using a small threshold, without creating
	// a 25 MiB fixture. The output itself is produced by the real engine.
	result, err := runWithInspect(context.Background(), Params{Workspace: workspace, YAML: string(yaml), Inputs: map[string]string{"parts": "parts.xlsx"}}, func(path string, p Params) (Preview, error) {
		return inspectWithLimit(path, p, 1)
	})
	if err != nil {
		t.Fatal(err)
	}
	r := result.(map[string]any)
	if _, ok := r["preview"]; ok || r["previewWarning"] != "Workbook must be a regular file of at most 25 MiB" {
		t.Fatalf("unexpected preview result: %+v", r)
	}
	artifact, err := os.ReadFile(filepath.Join(workspace, r["path"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(artifact)); r["sha256"] != got {
		t.Fatalf("hash mismatch: got %v, want %s", r["sha256"], got)
	}
}
func TestRunPreservesArtifactWhenPreviewExceedsFrameBudget(t *testing.T) {
	root := fixtureRoot(t)
	workspace := t.TempDir()
	data, err := os.ReadFile(filepath.Join(root, "examples/parts.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "parts.xlsx"), data, 0600); err != nil {
		t.Fatal(err)
	}
	yaml, err := os.ReadFile(filepath.Join(root, "examples/parts-summary.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := runWithInspect(context.Background(), Params{Workspace: workspace, YAML: string(yaml), Inputs: map[string]string{"parts": "parts.xlsx"}}, func(string, Params) (Preview, error) {
		return Preview{Rows: [][]string{{strings.Repeat("x", MaxFrameBytes)}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r := result.(map[string]any)
	if _, ok := r["preview"]; ok || r["previewWarning"] != "Workbook preview is too large to display" {
		t.Fatalf("unexpected preview result: %+v", r)
	}
	artifact, err := os.ReadFile(filepath.Join(workspace, r["path"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(artifact)); r["sha256"] != got {
		t.Fatalf("hash mismatch: got %v, want %s", r["sha256"], got)
	}
	encoded, err := json.Marshal(Response{Version: Version, ID: "test", OK: true, Result: result})
	if err != nil || len(encoded) > MaxFrameBytes {
		t.Fatalf("run response exceeds frame limit: %d bytes, error %v", len(encoded), err)
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

func TestTraceTheBOMOffline(t *testing.T) {
	root := fixtureRoot(t)
	yaml, err := os.ReadFile(filepath.Join(root, "workflows/bom.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	step := 4
	r := request(t, "trace", Params{Workspace: root, YAML: string(yaml), Step: &step, Limit: 20,
		Inputs: map[string]string{"bom": "testdata/bom_basic.xlsx", "priority": "testdata/prio_basic.xlsx"}})
	if !r.OK {
		t.Fatal(r.Error)
	}
	encoded, _ := json.Marshal(r.Result)
	if len(encoded) > MaxFrameBytes {
		t.Fatalf("trace frame %d bytes", len(encoded))
	}
	var out struct {
		Steps []struct {
			Name  string
			Total int
			Rows  [][]string
			Error string
		}
		Format string
	}
	json.Unmarshal(encoded, &out)
	if out.Format != "xlsx" || len(out.Steps) < 40 || out.Steps[4].Rows == nil || len(out.Steps[4].Rows) > 20 || out.Steps[3].Rows != nil {
		t.Fatalf("trace: %d steps, format %q", len(out.Steps), out.Format)
	}
	for _, s := range out.Steps {
		if s.Error != "" {
			t.Errorf("step %s failed offline: %s", s.Name, s.Error)
		}
	}
}

// With a mocks file in the workspace, previews and runs answer sql and rest
// steps from it, and a run can be kept as a workflow test.
func TestMocksAndSaveTest(t *testing.T) {
	root := fixtureRoot(t)
	workspace := t.TempDir()
	data, err := os.ReadFile(filepath.Join(root, "examples/parts.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	const source = `name: Enrich
input:
  parts: {label: Parts}
steps:
  - from: {input: parts, sheet: Parts, trim: true}
  - rest: {url: 'http://erp.internal/stock', rows: response.items, save: stock}
  - sql: {query: 'SELECT Code, Vendor FROM Vendors', save: vendors}
  - join: {with: stock, on: [Part Number], right_on: [code], take: {Stock: qty}}
  - join: {with: vendors, on: [Part Number], right_on: [Code], take: {Vendor: Vendor}}
  - save: out
output:
  sheets: {Out: out}
`
	const mocks = `rest:
  - url: /stock
    body: {items: [{code: "00123", qty: 7}]}
sql:
  - query: Vendors
    columns: [Code, Vendor]
    rows: [["00123", Acme]]
`
	for name, content := range map[string]string{"parts.xlsx": string(data), "workflows/enrich.yaml": source, "tests/enrich.mocks.yaml": mocks} {
		path := filepath.Join(workspace, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p := Params{Workspace: workspace, YAML: source, Inputs: map[string]string{"parts": "parts.xlsx"}, Mocks: "tests/enrich.mocks.yaml"}

	focus := 5
	p.Step = &focus
	r := request(t, "trace", p)
	if !r.OK {
		t.Fatalf("trace: %+v", r.Error)
	}
	traced := r.Result.(map[string]any)
	steps := traced["steps"].([]workflow.StepTrace)
	if traced["mocks"] != true || steps[5].Error != "" || len(steps[5].Rows) == 0 || !strings.Contains(strings.Join(steps[5].Rows[0], "|"), "7|Acme") {
		t.Fatalf("trace with mocks: %+v", steps[5])
	}
	// Without the file, the same request is an offline preview as before.
	p.Mocks = "tests/missing.mocks.yaml"
	if r := request(t, "trace", p); !r.OK || r.Result.(map[string]any)["mocks"] != false {
		t.Fatalf("trace without mocks: %+v", r)
	}
	p.Mocks, p.Step = "tests/enrich.mocks.yaml", nil
	if r := request(t, "run", p); !r.OK {
		t.Fatalf("run: %+v", r.Error)
	}

	p.Path, p.Name = "workflows/enrich.yaml", "One part in stock"
	r = request(t, "saveTest", p)
	if !r.OK {
		t.Fatalf("saveTest: %+v", r.Error)
	}
	saved := r.Result.(map[string]any)
	if saved["file"] != "tests/enrich.test.yaml" || saved["expected"] != "tests/enrich.one-part-in-stock.xlsx" {
		t.Fatalf("saved %v", saved)
	}
	p.Name = "Second case"
	if r := request(t, "saveTest", p); !r.OK {
		t.Fatalf("second saveTest: %+v", r.Error)
	}
	if r := request(t, "saveTest", p); r.OK || !strings.Contains(r.Error.Message, "already has a test named") {
		t.Fatalf("repeated name: %+v", r)
	}
	file, err := wftest.Load(filepath.Join(workspace, "tests/enrich.test.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	results := file.Run(context.Background(), false)
	if len(results) != 2 || results[0].Err != nil || results[1].Err != nil {
		t.Fatalf("the saved tests do not pass: %+v", results)
	}
	text, _ := os.ReadFile(filepath.Join(workspace, "tests/enrich.test.yaml"))
	for _, want := range []string{"workflow: ../workflows/enrich.yaml", "in: {parts: ../parts.xlsx}", "mocks: enrich.mocks.yaml", "# Tests for enrich.yaml"} {
		if !strings.Contains(string(text), want) {
			t.Errorf("the test file lacks %q:\n%s", want, text)
		}
	}
	p.Path = "../outside.yaml"
	if r := request(t, "saveTest", p); r.OK {
		t.Fatal("saveTest accepted a workflow outside the workspace")
	}
}
