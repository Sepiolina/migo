package bom

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"

	"miservice/internal/config"
	"miservice/internal/jsx"
	"miservice/internal/logx"
)

// testdata/golden_random7_nosap.xlsx is the output of the pre-refactor
// implementation for the same inputs (stubbed Material Master, SAP off). The
// workflows/bom.yaml must reproduce every cell value of both sheets, so run
// this after ANY change to a step or to the workflow file.
func TestOutputMatchesGolden(t *testing.T) {
	raw, err := os.ReadFile("../../../testdata/stub_random7.json")
	if err != nil {
		t.Fatal(err)
	}
	var stub map[string]struct{ MaterialNo, ZBlock string }
	if err := json.Unmarshal(raw, &stub); err != nil {
		t.Fatal(err)
	}
	lookup := func(_ context.Context, mpns []string) (map[string]Match, error) {
		res := map[string]Match{}
		for _, m := range mpns {
			if v, ok := stub[jsx.Norm(m)]; ok {
				res[jsx.Norm(m)] = Match{v.MaterialNo, v.ZBlock}
			}
		}
		return res, nil
	}
	cfg := &config.Config{DB: config.DB{Enabled: true}}
	out := filepath.Join(t.TempDir(), "out.xlsx")
	inputs := map[string]string{"bom": "../../../testdata/bom_random7.xls", "priority": "../../../testdata/prio_random7.xls"}
	if err := bomWorkflow(t, Deps{Lookup: lookup}).Run(context.Background(), cfg, logx.Nop, inputs, out); err != nil {
		t.Fatal(err)
	}
	got, want := readAll(t, out), readAll(t, "../../../testdata/golden_random7_nosap.xlsx")
	for sheet, w := range want {
		if !reflect.DeepEqual(got[sheet], w) {
			t.Errorf("sheet %q differs from the golden output", sheet)
		}
	}
}

func readAll(t *testing.T, path string) map[string][][]string {
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	res := map[string][][]string{}
	for _, name := range f.GetSheetList() {
		rows, err := f.GetRows(name)
		if err != nil {
			t.Fatal(err)
		}
		res[name] = rows
	}
	return res
}
