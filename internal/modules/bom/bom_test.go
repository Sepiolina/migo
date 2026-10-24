package bom

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"miservice/internal/config"
	"miservice/internal/logx"
)

func TestHelpers(t *testing.T) {
	for in, want := range map[string]bool{"PCB": true, "PCB, MAIN": true, "PCBA": false, "pcb assy": true, "CAP, PCB": true, "X/PCB1": false, "": false, "ASSY|PCB": true} {
		if IsPcbItem(in) != want {
			t.Errorf("IsPcbItem(%q) want %v", in, want)
		}
	}
	if ExtractRevToken(" B2 x ") != "B2" || ExtractRevToken("") != "" {
		t.Error("rev token")
	}
	if ReformatPcbMpn("PN", "B") != "PN_REV_B" || ReformatPcbMpn("PN_REV_B", "B") != "PN_REV_B" || ReformatPcbMpn("", "B") != "" {
		t.Error("reformat")
	}
	a, b := IdentifyUploadedFiles(Uploaded{"1", "Shortage.xlsx"}, Uploaded{"2", "my.xls"})
	if a.Path != "2" || b.Path != "1" {
		t.Error("swap")
	}
	a, _ = IdentifyUploadedFiles(Uploaded{"1", "x.xlsx"}, Uploaded{"2", "y.xlsx"})
	if a.Path != "1" {
		t.Error("keep order")
	}
	for _, bad := range []string{"", "../x", "a b", "123456789012345678901"} {
		if _, ok := ParseEmployeeNumber(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
	if v, ok := ParseEmployeeNumber(" 113280 "); !ok || v != "113280" {
		t.Error("employee")
	}
	if BuildOutputFilename("Customer BOM.v2.xls", "9") != "Customer BOM.v2_9.xlsx" ||
		BuildOutputFilename(`a:b*c.xls`, "9") != "a_b_c_9.xlsx" || BuildOutputFilename(".xls", "9") != ".xls_9.xlsx" {
		t.Error("output filename")
	}
}

func row(desc, mfg, mpn string) *Row {
	return &Row{Description: desc, EffectiveDescription: desc, MfgName: mfg, MfrPN: mpn, InScope: true}
}

func TestVendorPolicy(t *testing.T) {
	rows := []*Row{
		row("CAP X", "TDK", "CGA3E"), row("CAP X", "TDK", "NOPE"), row("IND", "SAMSUNG", "any"),
		row("CAP X", "SEMCO", "CL10B104"), row("CAP X", "Samsung Electro", "CL10B104PXYZ"),
		row("RES", "yageo corporation", "x"), row("RES", "Murata", "x"),
	}
	rows = append(rows, &Row{Description: "RES", MfgName: "KDS", InScope: false})
	res := RunVendorPolicy(rows, DefaultVendorPolicy)
	want := []bool{false, true, false, true, false, true, false, false}
	for i, w := range want {
		if rows[i].IsForbidden != w {
			t.Errorf("row %d forbidden=%v want %v", i, rows[i].IsForbidden, w)
		}
	}
	if res.NForbidden != 3 || res.ExemptCount["TDK"] != 1 || res.ExemptCount["SAMSUNG"] != 2 {
		t.Errorf("%+v", res)
	}
}

// A new brand rule is data: this is all it takes (see docs/EXTENDING.md).
func TestVendorPolicyCustomException(t *testing.T) {
	p := &VendorPolicy{
		BannedUnconditional: []string{"ACME"},
		BrandExceptions: []BrandException{{
			Name: "VISHAY", LogLabel: "Vishay Exception", LogSuffix: "Vishay CAP row(s) confirmed valid.",
			BrandMatch: brandContainsAny("VISHAY"),
			IsExempt:   capsMustSatisfy(mpnStartsWith("VJ")),
		}},
	}
	rows := []*Row{row("CAP", "Vishay", "VJ0603"), row("CAP", "Vishay", "XX"), row("RES", "Vishay", "XX"), row("RES", "acme", "x")}
	res := RunVendorPolicy(rows, p)
	if rows[0].IsForbidden || !rows[1].IsForbidden || rows[2].IsForbidden || !rows[3].IsForbidden || res.ExemptCount["VISHAY"] != 2 {
		t.Errorf("%v %v %v %v %+v", rows[0].IsForbidden, rows[1].IsForbidden, rows[2].IsForbidden, rows[3].IsForbidden, res)
	}
	var lines []string
	LogVendorPolicyResult(res, p, func(s string) { lines = append(lines, s) })
	if len(lines) != 2 || lines[0] != "Vishay Exception: 2 Vishay CAP row(s) confirmed valid." || lines[1] != "CFT-001: 2 row(s) flagged as forbidden." {
		t.Errorf("%q", lines)
	}
}

func TestGroupingAndQtyFix(t *testing.T) {
	rows := []*Row{
		{Level: "0", PartNumber: "TOP", Rev: "A", InScope: true},
		{FindNum: "1", Level: "1", PartNumber: "P1", Description: "D", Qty: "0", UOM: "EA", RefDes: "C1", MfgName: "M", MfrPN: "a", InScope: true},
		{RefDes: "C2", MfgName: "N", MfrPN: "b", Qty: "5", InScope: true},
		{FindNum: "2", Qty: "0", RefDes: "R1", MfrPN: "c", InScope: true},
	}
	ProcessGrouping(rows)
	if !rows[0].IsLevel0 || rows[0].GroupID != 1 || rows[1].GroupID != 2 || rows[2].GroupID != 2 || rows[3].GroupID != 3 {
		t.Errorf("groups %d %d %d %d", rows[0].GroupID, rows[1].GroupID, rows[2].GroupID, rows[3].GroupID)
	}
	if rows[1].EffectiveQty != "5" || rows[3].EffectiveQty != "1" {
		t.Errorf("qty fix %q %q", rows[1].EffectiveQty, rows[3].EffectiveQty)
	}
	if rows[2].EffectiveDescription != "D" || rows[2].DisplayFindNum != "1" {
		t.Errorf("fill-down %q %q", rows[2].EffectiveDescription, rows[2].DisplayFindNum)
	}
}

func TestScopeStopsAtLastPcbRow(t *testing.T) {
	rows := []*Row{{EffectiveDescription: "CAP"}, {EffectiveDescription: "PCB, main"}, {EffectiveDescription: "RES"}}
	markScope(rows)
	if !rows[0].InScope || !rows[1].InScope || rows[2].InScope {
		t.Error("rows after the last PCB are out of scope")
	}
	noPcb := []*Row{{EffectiveDescription: "CAP"}, {EffectiveDescription: "RES"}}
	markScope(noPcb)
	if !noPcb[0].InScope || !noPcb[1].InScope {
		t.Error("no PCB row: everything in scope")
	}
}

func cftRow(idx int, fpn string) *Row {
	return &Row{OriginalIndex: idx, GroupID: 1, InScope: true, HasIJK: true, ForthPN: fpn, MfgName: "X"}
}

func TestCft002Duplicates(t *testing.T) {
	a, b, c, d := cftRow(1, "F1"), cftRow(2, "F2"), cftRow(3, "F1"), cftRow(4, "CREATE P/N")
	one := 1.0
	c.PriorityValue = &one
	rows := []*Row{a, b, c, d}
	RunCft002(rows, PriorityMap{"F1": &one}, DefaultVendorPolicy, logx.Nop)
	if !c.IsDuplicateForthPN || a.IsDuplicateForthPN || d.IsDuplicateForthPN {
		t.Error("duplicate flags")
	}
	if a.Priority != "MAIN" || b.Priority != "SUB1" || d.Priority != "SUB2" || c.Priority != "" {
		t.Errorf("priorities %q %q %q %q", a.Priority, b.Priority, d.Priority, c.Priority)
	}
	if c.DebugReason != "Duplicate Forth P/N (excluded from MAIN/SUB)" || a.DebugReason != "MAIN (Cond A: Single P1 match)" {
		t.Errorf("reasons %q / %q", c.DebugReason, a.DebugReason)
	}
}

func TestCft002StockTieBreak(t *testing.T) {
	r1, r2 := cftRow(1, "F1"), cftRow(2, "F2")
	r1.Stock, r2.Stock = 10, 10
	r1.VName, r2.VName = "V1", "V2"
	RunCft002([]*Row{r1, r2}, PriorityMap{}, DefaultVendorPolicy, logx.Nop)
	if r1.Priority != "MAIN" || r2.Priority != "SUB1" || r1.DebugReason != "MAIN (Cond C: No P1 -> Vendor match [V1, top-most])" {
		t.Errorf("%q %q %q", r1.Priority, r2.Priority, r1.DebugReason)
	}
}

func pcbRows() []*Row {
	r := row("PCB, board", "ACME", "PCB-1_REV_B")
	r.HasIJK = true
	return []*Row{r}
}

func jobWith(rows []*Row, db bool, log logx.Logger) *Job {
	return &Job{Cfg: &config.Config{DB: config.DB{Enabled: db}}, Policy: DefaultVendorPolicy, Log: log, Rows: rows}
}

func TestDBLookupPcbSanmina(t *testing.T) {
	var calls [][]string
	lookup := func(_ context.Context, m []string) (map[string]Match, error) {
		calls = append(calls, m)
		if len(m) == 1 && m[0] == "PCB-1_REV_B_SANMINA" {
			return map[string]Match{"PCB-1_REV_B_SANMINA": {"F9", "Z1"}}, nil
		}
		return map[string]Match{}, nil
	}
	rows := pcbRows()
	lookupForthPNs(context.Background(), jobWith(rows, true, logx.Nop), lookup)
	if len(calls) != 2 || rows[0].ForthPN != "F9" || rows[0].MfrPN != "PCB-1_REV_B_SANMINA" || !rows[0].IsZ1 {
		t.Errorf("calls=%v row=%+v", calls, rows[0])
	}
}

func TestDBLookupFailureFallsBack(t *testing.T) {
	var logged string
	rows := []*Row{row("RES", "M", "x"), row("RES", "YAGEO CORPORATION", "y")}
	for _, r := range rows {
		r.HasIJK = true
	}
	rows[1].IsForbidden = true
	lookup := func(context.Context, []string) (map[string]Match, error) { return nil, errors.New("boom") }
	lookupForthPNs(context.Background(), jobWith(rows, true, func(s string) { logged = s }), lookup)
	if rows[0].ForthPN != "CREATE P/N" || rows[1].ForthPN != "" || logged == "" {
		t.Errorf("%q %q %q", rows[0].ForthPN, rows[1].ForthPN, logged)
	}
}

func TestDBDisabledUsesFallback(t *testing.T) {
	rows := []*Row{row("RES", "M", "x")}
	rows[0].HasIJK = true
	lookupForthPNs(context.Background(), jobWith(rows, false, logx.Nop), func(context.Context, []string) (map[string]Match, error) {
		t.Fatal("lookup must not run when the DB is disabled")
		return nil, nil
	})
	if rows[0].ForthPN != "CREATE P/N" {
		t.Error("fallback")
	}
}

func TestResultMap(t *testing.T) {
	m := BuildResultMap([]MasterRecord{{"1", "", " abc "}, {"2", "Z1", "ABC"}, {"3", "", "  "}}, nil)
	if len(m) != 1 || m["ABC"].MaterialNo != "1" {
		t.Errorf("%v", m)
	}
}

// ---- pipeline wiring ---------------------------------------------------------

func TestDefaultStageOrder(t *testing.T) {
	var names []string
	for _, s := range DefaultStages(Deps{}) {
		names = append(names, s.Name)
	}
	want := []string{"read-inputs", "group-rows", "pcb-scope", "pcb-reformat", "cft-001-vendor-policy",
		"material-master", "sap-enrich", "cft-002-priority", "write-workbook"}
	if len(names) != len(want) {
		t.Fatalf("%v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("stage %d = %s, want %s", i, names[i], want[i])
		}
	}
}

// Adding a feature = adding a Stage. This one marks rows and stops the run
// before anything is written.
func TestCustomStageAndAbort(t *testing.T) {
	var ran []string
	p := &Pipeline{Cfg: &config.Config{}, Stages: []Stage{
		{"first", func(context.Context, *Job) error { ran = append(ran, "first"); return nil }},
		{"boom", func(context.Context, *Job) error { ran = append(ran, "boom"); return errors.New("bad input") }},
		{"never", func(context.Context, *Job) error { ran = append(ran, "never"); return nil }},
	}}
	err := p.Process(context.Background(), "a", "b", "c")
	if err == nil || err.Error() != "bad input" || len(ran) != 2 {
		t.Errorf("err=%v ran=%v", err, ran)
	}
}

func TestPipelineRealFilesNoExternals(t *testing.T) {
	out := filepath.Join(t.TempDir(), "o.xlsx")
	var lines []string
	p := NewPipeline(&config.Config{}, func(s string) { lines = append(lines, s) }, Deps{})
	if err := p.Process(context.Background(), "../../../testdata/bom_basic.xlsx", "../../../testdata/prio_basic.xlsx", out); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(out); err != nil || st.Size() == 0 {
		t.Error("workbook not written")
	}
	if len(lines) == 0 || lines[len(lines)-1] != "Successfully processed and generated: "+out {
		t.Errorf("%q", lines)
	}
}
