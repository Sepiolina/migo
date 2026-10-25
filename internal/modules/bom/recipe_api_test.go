package bom_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
	"miservice/internal/app"
	"miservice/internal/config"
	"miservice/internal/logx"
	"miservice/internal/modules/bom"
)

func apiWorkbook(t *testing.T, url string) *excelize.File {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("download %d: %s", resp.StatusCode, b)
	}
	f, err := excelize.OpenReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func apiRows(t *testing.T, f *excelize.File) map[string][][]string {
	t.Helper()
	result := map[string][][]string{}
	for _, name := range f.GetSheetList() {
		rows, err := f.GetRows(name, excelize.Options{RawCellValue: true})
		if err != nil {
			t.Fatal(err)
		}
		result[name] = rows
	}
	return result
}

func TestYAMLAPISelectionAndLegacyParity(t *testing.T) {
	dir := t.TempDir()
	recipe, err := os.ReadFile("default.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// A deliberate, visible edit: omit priority assignment. If the server were
	// still calling the entire fixed Go pipeline, this test would fail.
	variant := strings.Replace(string(recipe), "- stage: cft-002-priority", "- stage: cft-002-priority\n    enabled: false", 1)
	if err := os.WriteFile(filepath.Join(dir, "custom.yaml"), []byte(variant), 0600); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(dir, "config.json")
	if err := os.WriteFile(conf, []byte(`{"db_enabled":false,"sap_enabled":false,"bom_pipelines":{"custom":"custom.yaml"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(conf)
	if err != nil {
		t.Fatal(err)
	}
	a, err := app.New(cfg, logx.Nop, app.DefaultModules)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	files := [][2]string{{"Customer BOM.xlsx", "../../../testdata/bom_basic.xlsx"}, {"Priority.xlsx", "../../../testdata/prio_basic.xlsx"}}
	code, result := post(t, server.URL+"/api/bom/upload", map[string]string{"bam": "113280"}, files...)
	if code != 200 || result["fileName"] != "Customer BOM_113280.xlsx" || result["downloadUrl"] != server.URL+"/api/bom/download" {
		t.Fatalf("legacy response %d %v", code, result)
	}
	legacy := apiWorkbook(t, result["downloadUrl"])
	reference := filepath.Join(dir, "reference.xlsx")
	if err := bom.NewPipeline(cfg, logx.Nop, bom.Deps{}).Process(context.Background(), files[0][1], files[1][1], reference); err != nil {
		t.Fatal(err)
	}
	expected, err := excelize.OpenFile(reference)
	if err != nil {
		t.Fatal(err)
	}
	defer expected.Close()
	if !reflect.DeepEqual(legacy.GetSheetList(), expected.GetSheetList()) || !reflect.DeepEqual(apiRows(t, legacy), apiRows(t, expected)) {
		t.Fatal("default YAML API changed legacy workbook contents")
	}
	for name, rows := range apiRows(t, expected) {
		for r, row := range rows {
			for c := range row {
				cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
				actualStyle, err := legacy.GetCellStyle(name, cell)
				if err != nil {
					t.Fatal(err)
				}
				expectedStyle, err := expected.GetCellStyle(name, cell)
				if err != nil {
					t.Fatal(err)
				}
				actual, err := legacy.GetStyle(actualStyle)
				if err != nil {
					t.Fatal(err)
				}
				want, err := expected.GetStyle(expectedStyle)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(actual, want) {
					t.Fatalf("API changed style %s!%s", name, cell)
				}
			}
		}
	}
	code, result = post(t, server.URL+"/api/pipelines/custom/upload", map[string]string{"bam": "113280"}, files...)
	if code != 200 || result["downloadUrl"] != server.URL+"/api/pipelines/custom/download" || !strings.HasPrefix(result["fileName"], "pipeline_custom_") {
		t.Fatalf("custom response %d %v", code, result)
	}
	custom := apiWorkbook(t, result["downloadUrl"])
	if reflect.DeepEqual(apiRows(t, custom), apiRows(t, legacy)) {
		t.Fatal("YAML stage edit did not change executed API output")
	}
	// Per-profile download state and output names must not overwrite the legacy result.
	if !reflect.DeepEqual(apiRows(t, apiWorkbook(t, server.URL+"/api/bom/download")), apiRows(t, legacy)) {
		t.Fatal("custom output replaced legacy download")
	}
	entries, err := os.ReadDir(filepath.Join(dir, "uploads"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary inputs leaked: %v", entries)
	}

	// A client can choose a filename identical to the custom prefix. Physical
	// storage namespaces, not naming conventions, must prevent overwrites.
	collidingFiles := [][2]string{{"pipeline_custom_Customer BOM.xlsx", files[0][1]}, files[1]}
	code, legacyCollision := post(t, server.URL+"/api/bom/upload", map[string]string{"bam": "113280"}, collidingFiles...)
	if code != 200 {
		t.Fatalf("collision legacy upload %d %v", code, legacyCollision)
	}
	code, customCollision := post(t, server.URL+"/api/pipelines/custom/upload", map[string]string{"bam": "113280"}, files...)
	if code != 200 {
		t.Fatalf("collision custom upload %d %v", code, customCollision)
	}
	if legacyCollision["fileName"] != customCollision["fileName"] {
		t.Fatal("fixture did not exercise same storage filename")
	}
	if !reflect.DeepEqual(apiRows(t, apiWorkbook(t, legacyCollision["downloadUrl"])), apiRows(t, legacy)) {
		t.Fatal("custom pipeline overwrote legacy prefixed filename")
	}
	if !reflect.DeepEqual(apiRows(t, apiWorkbook(t, customCollision["downloadUrl"])), apiRows(t, custom)) {
		t.Fatal("legacy prefixed filename overwrote custom pipeline")
	}
	for _, path := range []string{filepath.Join(dir, "outputs", legacyCollision["fileName"]), filepath.Join(dir, "outputs", "pipelines", "custom", customCollision["fileName"])} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	// The same custom YAML can replace the original endpoint, without Go edits.
	cfg.BOMWorkflow = "custom.yaml"
	a, err = app.New(cfg, logx.Nop, app.DefaultModules)
	if err != nil {
		t.Fatal(err)
	}
	replacement := httptest.NewServer(a.Handler())
	defer replacement.Close()
	code, result = post(t, replacement.URL+"/api/bom/upload", map[string]string{"bam": "113280"}, files...)
	if code != 200 {
		t.Fatalf("configured route %d %v", code, result)
	}
	if !reflect.DeepEqual(apiRows(t, apiWorkbook(t, result["downloadUrl"])), apiRows(t, custom)) {
		t.Fatal("bom_workflow override was not used")
	}
}

func TestInvalidYAMLAPIPipelineFailsStartup(t *testing.T) {
	for _, name := range []string{"../bad", "Bad", "a/b", ""} {
		_, err := app.New(&config.Config{Dir: t.TempDir(), BOMPipelines: map[string]string{name: "missing.yaml"}}, logx.Nop, app.DefaultModules)
		if err == nil || !strings.Contains(err.Error(), "invalid BOM pipeline name") {
			t.Fatalf("name %q: %v", name, err)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("engine: bom\nworkflow: bad\nprocess: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []*config.Config{{Dir: dir, BOMWorkflow: "bad.yaml"}, {Dir: dir, BOMPipelines: map[string]string{"bad": "bad.yaml"}}} {
		if _, err := app.New(cfg, logx.Nop, app.DefaultModules); err == nil {
			t.Fatal("invalid recipe accepted at startup")
		}
	}
}

func TestBOMPipelineConfigValidation(t *testing.T) {
	for _, body := range []string{`{"bom_workflow":3}`, `{"bom_workflow":""}`, `{"bom_pipelines":[]}`, `{"bom_pipelines":{"x":false}}`} {
		path := filepath.Join(t.TempDir(), "config.json")
		if !json.Valid([]byte(body)) {
			t.Fatal("invalid test JSON")
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.Load(path); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
