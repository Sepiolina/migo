package workflows_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"miservice/internal/app"
	"miservice/internal/config"
	"miservice/internal/logx"
	"miservice/internal/modules/workflows"
)

func newServer(t *testing.T, files ...[2]string) (*httptest.Server, string) {
	dir := t.TempDir()
	for _, f := range files {
		os.MkdirAll(filepath.Join(dir, "workflows"), 0o755)
		if err := os.WriteFile(filepath.Join(dir, "workflows", f[0]), []byte(f[1]), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := app.New(&config.Config{Dir: dir}, logx.Nop, app.DefaultModules)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(a.Handler())
	t.Cleanup(ts.Close)
	return ts, dir
}

func post(t *testing.T, url string, fields map[string]string, files ...[2]string) (int, map[string]string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	for _, f := range files {
		fw, _ := mw.CreateFormFile("files", f[0])
		b, err := os.ReadFile(f[1])
		if err != nil {
			t.Fatal(err)
		}
		fw.Write(b)
	}
	mw.Close()
	resp, err := http.Post(url, mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]string
	b, _ := io.ReadAll(resp.Body)
	json.Unmarshal(b, &out)
	return resp.StatusCode, out
}

func TestEndpoints(t *testing.T) {
	ts, dir := newServer(t)
	resp, _ := http.Get(ts.URL + "/api/health")
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(b) != `{"status":"online","service":"BOM Processor Node.js API"}` {
		t.Errorf("health %d %s", resp.StatusCode, b)
	}
	resp, _ = http.Get(ts.URL + "/api/bom/download")
	if resp.StatusCode != 404 {
		t.Error("download before upload must be 404")
	}
	bomF, prioF := [2]string{"Customer BOM.xlsx", "../../../testdata/bom_basic.xlsx"}, [2]string{"Priority.xlsx", "../../../testdata/prio_basic.xlsx"}
	if c, b := post(t, ts.URL+"/api/bom/upload", map[string]string{"bam": "113280"}, bomF); c != 400 || b["error"] != "Expected 2 files (Customer BOM & Priority)" {
		t.Errorf("one file: %d %v", c, b)
	}
	if c, b := post(t, ts.URL+"/api/bom/upload", nil, bomF, prioF); c != 400 || b["error"] == "" {
		t.Errorf("no bam: %d %v", c, b)
	}
	if c, _ := post(t, ts.URL+"/api/bom/upload", map[string]string{"bam": "../x"}, bomF, prioF); c != 400 {
		t.Error("unsafe bam")
	}
	if c, b := post(t, ts.URL+"/api/bom/upload", map[string]string{"bam": "1"}, bomF, prioF, prioF); c != 500 || b["message"] != "Unexpected field" {
		t.Errorf("three files: %d %v", c, b)
	}
	c, out := post(t, ts.URL+"/api/bom/upload", map[string]string{"bam": "113280"}, prioF, bomF) // reversed order
	if c != 200 || out["status"] != "success" || out["fileName"] != "Customer BOM_113280.xlsx" || out["bam"] != "113280" {
		t.Fatalf("upload: %d %v", c, out)
	}
	resp, _ = http.Get(ts.URL + "/api/bom/download")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Disposition") == "" {
		t.Errorf("download %d %v", resp.StatusCode, resp.Header)
	}
	if left, _ := os.ReadDir(filepath.Join(dir, "uploads")); len(left) != 0 {
		t.Errorf("uploads not cleaned: %d files", len(left))
	}
	if c, _ := post(t, ts.URL+"/api/bom/upload", map[string]string{"bam": "1"}, [2]string{"a.xlsx", "../../../testdata/bom_basic.xlsx"}, [2]string{"b.xlsx", "../../../testdata/bom_basic.xlsx"}); c != 500 {
		t.Errorf("missing Shortage sheet should be 500, got %d", c)
	}
}

// Every workflow file is its own endpoint pair with its own latest download.
func TestWorkflowFileIsAnEndpoint(t *testing.T) {
	ts, dir := newServer(t, [2]string{"parts.yaml", `name: Parts
input:
  parts: {label: Parts list}
steps:
  - from: parts
  - filter: 'col("Part Number") != ""'
  - set: {Copy: 'trim(col("Part Number"))'}
  - save: cleaned
output:
  sheets: {Parts: cleaned}
`})
	if c, b := post(t, ts.URL+"/api/parts/upload", map[string]string{"bam": "7"}); c != 400 || b["error"] != "Expected 1 file (Parts list)" {
		t.Errorf("no file: %d %v", c, b)
	}
	c, out := post(t, ts.URL+"/api/parts/upload", map[string]string{"bam": "7"}, [2]string{"My BOM.xlsx", "../../../examples/parts.xlsx"})
	if c != 200 || out["message"] != "Parts processed successfully." || out["fileName"] != "My BOM_7.xlsx" || !strings.HasSuffix(out["downloadUrl"], "/api/parts/download") {
		t.Fatalf("upload: %d %v", c, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "outputs", "parts", "My BOM_7.xlsx")); err != nil {
		t.Error(err)
	}
	if resp, _ := http.Get(ts.URL + "/api/parts/download"); resp.StatusCode != 200 {
		t.Errorf("parts download %d", resp.StatusCode)
	}
	if resp, _ := http.Get(ts.URL + "/api/bom/download"); resp.StatusCode != 404 {
		t.Errorf("bom download must not see the parts result: %d", resp.StatusCode)
	}
}

func TestBadWorkflowFileStopsStartup(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "workflows"), 0o755)
	os.WriteFile(filepath.Join(dir, "workflows", "x.yaml"), []byte("name: X\ninput: {a: {}}\nsteps: [nope]\n"), 0o600)
	if _, err := app.New(&config.Config{Dir: dir}, logx.Nop, app.DefaultModules); err == nil || !strings.Contains(err.Error(), `unknown step "nope"`) {
		t.Errorf("err=%v", err)
	}
}

func TestHelpers(t *testing.T) {
	for _, bad := range []string{"", "../x", "a b", "123456789012345678901"} {
		if _, ok := workflows.ParseEmployeeNumber(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
	if v, ok := workflows.ParseEmployeeNumber(" 113280 "); !ok || v != "113280" {
		t.Error("employee")
	}
	if workflows.BuildOutputFilename("Customer BOM.v2.xls", "9") != "Customer BOM.v2_9.xlsx" ||
		workflows.BuildOutputFilename(`a:b*c.xls`, "9") != "a_b_c_9.xlsx" || workflows.BuildOutputFilename(".xls", "9") != ".xls_9.xlsx" {
		t.Error("output filename")
	}
}
