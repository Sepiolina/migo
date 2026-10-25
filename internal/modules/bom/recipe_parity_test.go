package bom

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/xuri/excelize/v2"

	"miservice/internal/clients/sap"
	"miservice/internal/config"
	"miservice/internal/jsx"
	"miservice/internal/logx"
)

// NewPipeline is the independent, unchanged Go reference. These tests execute
// real readers and writers on both paths instead of comparing stage names or
// replacing the stages with test functions.
func TestDefaultRecipeWorkbookParity(t *testing.T) {
	for _, tc := range []struct {
		name, fixture string
		db, sap       bool
	}{
		{"xlsx_no_externals", "basic.xlsx", false, false},
		{"xls_material_master_golden", "random7.xls", true, false},
		{"xls_material_master_sap", "random7.xls", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{DB: config.DB{Enabled: tc.db}}
			var lookupCalls [][]string
			deps := Deps{Lookup: parityLookup(t, &lookupCalls)}
			var sapMu sync.Mutex
			var sapCalls [][]map[string]string
			if tc.sap {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var request struct {
						Tcode      string              `json:"tcode"`
						EmployeeID string              `json:"employee_id"`
						Data       []map[string]string `json:"DATA"`
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Errorf("SAP request: %v", err)
						http.Error(w, "invalid request", http.StatusBadRequest)
						return
					}
					if r.Method != http.MethodPost || r.URL.Path != "/api/sap-rfc/call" || request.Tcode != "PARITY" || request.EmployeeID != "106049" || len(request.Data) < 2 || request.Data[0]["WERKS"] != "1000" {
						t.Errorf("unexpected SAP request: %s %s %+v", r.Method, r.URL.Path, request)
					}
					sapMu.Lock()
					sapCalls = append(sapCalls, request.Data)
					sapMu.Unlock()
					var records []map[string]any
					for _, row := range request.Data[1:] {
						status := ""
						if row["MATNR"] == "F1007" {
							status = "Z1"
						}
						records = append(records, map[string]any{" MATNR ": row["MATNR"], "MMSTA": status, "VNAME": "PARITY VENDOR", "LIFNR": "V137", "LABST": 137})
					}
					w.Header().Set("Content-Type", "application/json")
					if err := json.NewEncoder(w).Encode(map[string]any{"rows": records}); err != nil {
						t.Errorf("SAP response: %v", err)
					}
				}))
				defer server.Close()
				host, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
				if err != nil {
					t.Fatal(err)
				}
				cfg.SAP = config.SAP{Enabled: true, Host: host, Port: port, Tcode: "PARITY", Werks: "1000", BatchSize: 2, Retries: 1}
				deps.SAP = &sap.Client{HTTP: server.Client()}
			}
			recipe, err := DefaultRecipe()
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := recipe.Build(cfg, logx.Nop, deps)
			if err != nil {
				t.Fatal(err)
			}
			outDir := t.TempDir()
			legacyPath, recipePath := filepath.Join(outDir, "legacy.xlsx"), filepath.Join(outDir, "recipe.xlsx")
			bomPath, priorityPath := "../../../testdata/bom_"+tc.fixture, "../../../testdata/prio_"+tc.fixture
			if err := NewPipeline(cfg, logx.Nop, deps).Process(context.Background(), bomPath, priorityPath, legacyPath); err != nil {
				t.Fatal(err)
			}
			legacyLookupCalls := append([][]string(nil), lookupCalls...)
			lookupCalls = nil
			sapMu.Lock()
			legacySAPCalls := append([][]map[string]string(nil), sapCalls...)
			sapCalls = nil
			sapMu.Unlock()
			if err := compiled.Process(context.Background(), bomPath, priorityPath, recipePath); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(lookupCalls, legacyLookupCalls) || (tc.db && len(lookupCalls) == 0) || (!tc.db && len(lookupCalls) != 0) {
				t.Errorf("material-master calls: recipe=%v legacy=%v", lookupCalls, legacyLookupCalls)
			}
			sapMu.Lock()
			if !reflect.DeepEqual(sapCalls, legacySAPCalls) || (tc.sap && len(sapCalls) == 0) {
				t.Errorf("SAP calls: recipe=%v legacy=%v", sapCalls, legacySAPCalls)
			}
			sapMu.Unlock()
			assertParityWorkbook(t, legacyPath, recipePath)
			if tc.db && !tc.sap {
				// Keep the checked-in pre-refactor golden as a third independent
				// oracle; never regenerate it from either pipeline under test.
				if got, want := readAll(t, recipePath), readAll(t, "../../../testdata/golden_random7_nosap.xlsx"); !reflect.DeepEqual(got, want) {
					t.Error("recipe values differ from checked-in pre-refactor golden")
				}
			}
			if tc.sap {
				rows := readAll(t, recipePath)["debug"]
				found := false
				for _, row := range rows[1:] {
					if len(row) > 22 && row[20] == "PARITY VENDOR" && row[21] == "V137" && row[22] == "137" {
						found = true
					}
				}
				if !found {
					t.Fatal("SAP vendor, vendor code and numeric stock did not reach the workbook")
				}
			}
		})
	}
}

func parityLookup(t *testing.T, calls *[][]string) LookupFunc {
	t.Helper()
	data, err := os.ReadFile("../../../testdata/stub_random7.json")
	if err != nil {
		t.Fatal(err)
	}
	var records map[string]Match
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatal(err)
	}
	return func(_ context.Context, mpns []string) (map[string]Match, error) {
		*calls = append(*calls, append([]string(nil), mpns...))
		matches := map[string]Match{}
		for _, mpn := range mpns {
			if match, ok := records[jsx.Norm(mpn)]; ok {
				matches[jsx.Norm(mpn)] = match
			}
		}
		return matches, nil
	}
}

func assertParityWorkbook(t *testing.T, legacyPath, recipePath string) {
	t.Helper()
	legacy, err := excelize.OpenFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	recipe, err := excelize.OpenFile(recipePath)
	if err != nil {
		t.Fatal(err)
	}
	defer recipe.Close()
	wantSheets := []string{"BOM", "debug"}
	for _, f := range []*excelize.File{legacy, recipe} {
		if got := f.GetSheetList(); !reflect.DeepEqual(got, wantSheets) {
			t.Fatalf("sheet names/order: got %v want %v", got, wantSheets)
		}
	}
	for _, sheet := range wantSheets {
		wantRows, err := legacy.GetRows(sheet)
		if err != nil {
			t.Fatal(err)
		}
		gotRows, err := recipe.GetRows(sheet)
		if err != nil {
			t.Fatal(err)
		}
		if len(wantRows) < 2 || len(gotRows) != len(wantRows) {
			t.Fatalf("%s row count: recipe=%d legacy=%d", sheet, len(gotRows), len(wantRows))
		}
		for row := 1; row <= len(wantRows); row++ {
			wantHeight, err := legacy.GetRowHeight(sheet, row)
			if err != nil {
				t.Fatal(err)
			}
			gotHeight, err := recipe.GetRowHeight(sheet, row)
			if err != nil || gotHeight != wantHeight {
				t.Errorf("%s row %d height: got %v want %v (error %v)", sheet, row, gotHeight, wantHeight, err)
			}
			for col := 1; col <= 27; col++ {
				axis, _ := excelize.CoordinatesToCellName(col, row)
				want, got := parityCell(t, legacy, sheet, axis), parityCell(t, recipe, sheet, axis)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s!%s cell differs:\nrecipe: %+v\nlegacy: %+v", sheet, axis, got, want)
				}
			}
		}
		for col := 1; col <= 27; col++ {
			name, _ := excelize.ColumnNumberToName(col)
			wantWidth, err := legacy.GetColWidth(sheet, name)
			if err != nil {
				t.Fatal(err)
			}
			gotWidth, err := recipe.GetColWidth(sheet, name)
			if err != nil || gotWidth != wantWidth {
				t.Errorf("%s column %s width: got %v want %v (error %v)", sheet, name, gotWidth, wantWidth, err)
			}
		}
		wantPanes, err := legacy.GetPanes(sheet)
		if err != nil {
			t.Fatal(err)
		}
		gotPanes, err := recipe.GetPanes(sheet)
		if err != nil || !reflect.DeepEqual(gotPanes, wantPanes) {
			t.Errorf("%s panes differ: got %+v want %+v (error %v)", sheet, gotPanes, wantPanes, err)
		}
	}
	// Compare every workbook ZIP part, including styled empty cells, dimensions,
	// column styles, merges, print settings and relationships. ZIP metadata is
	// irrelevant; docProps may contain creation/modification timestamps.
	if got, want := parityWorkbookParts(t, recipePath), parityWorkbookParts(t, legacyPath); !reflect.DeepEqual(got, want) {
		for name, wantPart := range want {
			if got[name] != wantPart {
				t.Errorf("workbook ZIP part %q differs", name)
			}
		}
		for name := range got {
			if _, ok := want[name]; !ok {
				t.Errorf("unexpected workbook ZIP part %q", name)
			}
		}
	}
}

type parityCellValue struct {
	Raw, Display, Formula string
	Type                  excelize.CellType
	Style                 *excelize.Style
}

func parityCell(t *testing.T, f *excelize.File, sheet, axis string) parityCellValue {
	t.Helper()
	var result parityCellValue
	var err error
	check := func() {
		if err != nil {
			t.Fatal(fmt.Errorf("%s!%s: %w", sheet, axis, err))
		}
	}
	result.Raw, err = f.GetCellValue(sheet, axis, excelize.Options{RawCellValue: true})
	check()
	result.Display, err = f.GetCellValue(sheet, axis)
	check()
	result.Formula, err = f.GetCellFormula(sheet, axis)
	check()
	result.Type, err = f.GetCellType(sheet, axis)
	check()
	styleID, err := f.GetCellStyle(sheet, axis)
	check()
	result.Style, err = f.GetStyle(styleID)
	check()
	return result
}

func parityWorkbookParts(t *testing.T, path string) map[string]string {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	parts := map[string]string{}
	for _, entry := range r.File {
		if strings.HasPrefix(entry.Name, "docProps/") {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		parts[entry.Name] = string(data)
	}
	return parts
}
