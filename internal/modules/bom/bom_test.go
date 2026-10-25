package bom

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"miservice/internal/config"
	"miservice/internal/jsx"
	"miservice/internal/logx"
	"miservice/internal/workflow"
	builtin "miservice/workflows"
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
	res := RunVendorPolicy(rows, defaultPolicy(t))
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

// A new brand rule is data: in a workflow file it is the same four keys.
func TestVendorPolicyCustomException(t *testing.T) {
	p, err := VendorPolicySpec{
		Banned: []string{"ACME"},
		Exceptions: []VendorException{{
			Name: "VISHAY", Brand: `brand contains "VISHAY"`, Allow: `desc not startsWith "CAP" || mpn startsWith "VJ"`,
			Log: "Vishay Exception: {count} Vishay CAP row(s) confirmed valid.",
		}},
	}.Compile()
	if err != nil {
		t.Fatal(err)
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
	RunCft002(rows, PriorityMap{"F1": &one}, &VendorPolicy{}, logx.Nop)
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
	RunCft002([]*Row{r1, r2}, PriorityMap{}, &VendorPolicy{}, logx.Nop)
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
	return &Job{Cfg: &config.Config{DB: config.DB{Enabled: db}}, Policy: &VendorPolicy{}, Log: log, Rows: rows}
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

// ---- workflow wiring ---------------------------------------------------------

func TestBOMWorkflowSteps(t *testing.T) {
	want := []string{"read_bom", "read_priority", "group_rows", "pcb_scope", "pcb_reformat", "vendor_policy",
		"material_master", "sap_enrich", "select_priority", "write_bom"}
	if got := bomWorkflow(t, Deps{}).Steps; !reflect.DeepEqual(got, want) {
		t.Errorf("steps %v, want %v", got, want)
	}
}

func TestStepOrderIsChecked(t *testing.T) {
	for name, steps := range map[string]string{
		"needs":   "[read_bom, pcb_scope, write_bom]",
		"after":   "[read_bom, read_priority, group_rows, pcb_scope, pcb_reformat, {vendor_policy: {}}, select_priority, material_master, write_bom]",
		"no read": "[group_rows, write_bom]",
		"arg":     "[read_bom, {group_rows: x}, write_bom]",
	} {
		doc := "name: X\ninput: {bom: {}, priority: {}}\nsteps: " + steps + "\n"
		if _, err := workflow.Parse([]byte(doc), Ops(Deps{})); err == nil {
			t.Errorf("%s: accepted %s", name, steps)
		}
	}
}

func TestWorkflowRealFilesNoExternals(t *testing.T) {
	out := filepath.Join(t.TempDir(), "o.xlsx")
	var lines []string
	err := bomWorkflow(t, Deps{}).Run(context.Background(), &config.Config{}, func(s string) { lines = append(lines, s) },
		map[string]string{"bom": "../../../testdata/bom_basic.xlsx", "priority": "../../../testdata/prio_basic.xlsx"}, out)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(out); err != nil || st.Size() == 0 {
		t.Error("workbook not written")
	}
	if len(lines) == 0 || lines[len(lines)-1] != "Successfully processed and generated: "+out {
		t.Errorf("%q", lines)
	}
}

// bomWorkflow compiles the built-in workflows/bom.yaml.
func bomWorkflow(t *testing.T, d Deps) *workflow.Workflow {
	t.Helper()
	data, err := fs.ReadFile(builtin.FS, "bom.yaml")
	if err != nil {
		t.Fatal(err)
	}
	wf, err := workflow.Parse(data, workflow.TableOps().With(Ops(d)))
	if err != nil {
		t.Fatal(err)
	}
	return wf
}

// defaultPolicy is the vendor_policy of workflows/bom.yaml.
func defaultPolicy(t *testing.T) *VendorPolicy {
	t.Helper()
	data, err := fs.ReadFile(builtin.FS, "bom.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Steps []yaml.Node `yaml:"steps"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, st := range doc.Steps {
		if st.Kind == yaml.MappingNode && st.Content[0].Value == "vendor_policy" {
			n := st.Content[1]
			var spec VendorPolicySpec
			if err := n.Decode(&spec); err != nil {
				t.Fatal(err)
			}
			p, err := spec.Compile()
			if err != nil {
				t.Fatal(err)
			}
			return p
		}
	}
	t.Fatal("bom.yaml has no vendor_policy step")
	return nil
}

// The CFT-001 rules moved from Go into workflows/bom.yaml. These are the old
// Go matchers; the expressions must agree with them on every edge case.
func TestVendorPolicyMatchesFormerGoRules(t *testing.T) {
	tdkWord := regexp.MustCompile(`\bTDK\b`)
	isCap := func(r *Row) bool {
		d := r.EffectiveDescription
		if d == "" {
			d = r.Description
		}
		return strings.HasPrefix(jsx.Norm(d), "CAP")
	}
	clean := func(r *Row) string { return stripLeadingNonAlnum(jsx.Norm(r.MfrPN)) }
	old := func(r *Row) (forbidden bool) {
		b := jsx.Norm(r.MfgName)
		switch {
		case b != "" && strings.Contains(b, "TDK") && (tdkWord.MatchString(b) || strings.HasPrefix(b, "TDK")):
			return isCap(r) && !strings.HasPrefix(clean(r), "CGA")
		case b != "" && (strings.Contains(b, "SAMSUNG") || strings.Contains(b, "SEMCO")):
			c := []rune(clean(r))
			auto := len(c) >= 2 && c[0] == 'C' && c[1] == 'L' && (len(c) >= 13 && c[12] == 'P' || len(c) >= 9 && c[8] == 'P')
			return isCap(r) && !auto
		}
		return b == "YAGEO CORPORATION" || b == "WALSIN TECHNOLOGY CORPORATION" || b == "KDS"
	}
	brands := []string{"TDK", " tdk corp", "XTDKY", "TDK-LAMBDA", "EPCOS (TDK)", "SAMSUNG", "Samsung Electro", "semco", "SAMSUNGX", "kds", "Yageo Corporation", "", "MURATA"}
	descs := []string{"CAP CER", " cap", "RES", "", "CAPACITOR"}
	mpns := []string{"CGA3E", "--cga3e", "XCGA", "CL10B104PXYZ", "CL10B104KB8NNNC", "CL10B104KBP", "CL10B1P", "CLxxxxxxPyy", "CLÄÄÄÄÄÄP", "", "  -CL10B104KB8PNC"}
	policy := defaultPolicy(t)
	for _, b := range brands {
		for _, d := range descs {
			for _, m := range mpns {
				r := row(d, b, m)
				RunVendorPolicy([]*Row{r}, policy)
				if want := old(r); r.IsForbidden != want {
					t.Errorf("brand=%q desc=%q mpn=%q: forbidden=%v, former rule %v", b, d, m, r.IsForbidden, want)
				}
			}
		}
	}
}
