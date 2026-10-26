package bom

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"

	"miservice/internal/jsx"
)

// The material_master, sap_enrich and select_priority rules moved from Go
// into workflows/bom.yaml. The functions below are the former Go rules; the
// workflow's rules must reach the same result on random rows.

func formerEligible(r *Row) string {
	switch {
	case r.IsLevel0:
		return "Ineligible (Level 0)"
	case !r.HasIJK:
		return "Ineligible (No I,J,K)"
	case r.IsZ1:
		return "Ineligible (Z1 Blocked)"
	case r.IsForbidden:
		return "Ineligible (CFT-001 Forbidden)"
	}
	return "Eligible"
}

func formerHasVendor(r *Row) bool {
	vn := jsx.Trim(r.VName)
	if vn == "" {
		vn = jsx.Trim(r.VendorCode)
	}
	return vn != "" && vn != "0" && vn != "None" && vn != "-"
}

func formerChooseMain(c []*Row) (*Row, string) {
	isP1 := func(r *Row) bool { return r.PriorityValue != nil && *r.PriorityValue == 1 }
	p1 := filterRows(c, isP1)
	switch {
	case len(p1) == 1:
		return p1[0], "MAIN (Cond A: Single P1 match)"
	case len(p1) > 1:
		if wv := filterRows(p1, formerHasVendor); len(wv) > 0 {
			m := maxStock(wv)
			cand := withStock(wv, m)
			main := topMost(cand)
			if len(cand) > 1 {
				return main, fmt.Sprintf("MAIN (Cond B: Multiple P1 & Vendors -> Highest Stock: %s [%d tied, top-most: %s])", jsx.NumToString(m), len(cand), vendorLabel(main))
			}
			return main, fmt.Sprintf("MAIN (Cond B: Multiple P1 & Vendors -> Highest Stock: %s [%s])", jsx.NumToString(m), vendorLabel(main))
		}
		m := maxStock(p1)
		cand := withStock(p1, m)
		return topMost(cand), fmt.Sprintf("MAIN (Cond B: Multiple P1, No Vendor -> Highest Stock: %s [%d tied, top-most])", jsx.NumToString(m), len(cand))
	}
	if wv := filterRows(c, formerHasVendor); len(wv) > 0 {
		main := topMost(wv)
		return main, fmt.Sprintf("MAIN (Cond C: No P1 -> Vendor match [%s, top-most])", vendorLabel(main))
	}
	if m := maxStock(c); m > 0 {
		cand := withStock(c, m)
		return topMost(cand), fmt.Sprintf("MAIN (Cond C: No P1, No Vendor -> Highest Stock: %s [%d tied, top-most])", jsx.NumToString(m), len(cand))
	}
	return topMost(c), "MAIN (Cond D: No P1, No Vendor, No Stock -> Fallback top-most row)"
}

func formerDuplicateKey(r *Row) string {
	if k := jsx.Norm(r.ForthPN); k != createPN {
		return k
	}
	return ""
}

func formerSAPEligible(r *Row) bool {
	return r.InScope && !r.IsLevel0 && r.HasIJK && !r.IsForbidden && r.ForthPN != "" && r.ForthPN != createPN
}

func randomRow(rng *rand.Rand, i int) *Row {
	pick := func(v ...string) string { return v[rng.Intn(len(v))] }
	r := &Row{
		OriginalIndex: i, GroupID: 1, InScope: rng.Intn(6) > 0, IsLevel0: rng.Intn(8) == 0, HasIJK: rng.Intn(6) > 0,
		IsZ1: rng.Intn(8) == 0, IsForbidden: rng.Intn(8) == 0, MfgName: "ACME",
		ForthPN:    pick("", "CREATE P/N", "create p/n", "F1", "F2", " f1 ", "F3"),
		VName:      pick("", " ", "0", "None", "-", "V1", "V2"),
		VendorCode: pick("", "0", "C1", "-"),
		Stock:      []float64{0, 0, 5, 10, 10, 0.5}[rng.Intn(6)],
	}
	switch rng.Intn(4) {
	case 0:
		one := 1.0
		r.PriorityValue = &one
	case 1:
		two := 2.0
		r.PriorityValue = &two
	}
	return r
}

func TestCft002MatchesFormerGoRules(t *testing.T) {
	c, sap := defaultCft002(t), defaultSAP(t)
	empty := &VendorPolicy{}
	rng := rand.New(rand.NewSource(1))
	for n := 0; n < 3000; n++ {
		grp := make([]*Row, 1+rng.Intn(6))
		for i := range grp {
			grp[i] = randomRow(rng, rng.Intn(50))
		}
		c.markEligibility(grp, empty)
		var eligible []*Row
		for _, r := range grp {
			if want := formerEligible(r); r.DebugEligible != want {
				t.Fatalf("eligibility %+v: %q, former %q", r, r.DebugEligible, want)
			}
			if r.DebugEligible == "Eligible" {
				eligible = append(eligible, r)
			}
		}
		for _, r := range grp {
			if got, want := c.dupKey.text(envOf(r, empty)), formerDuplicateKey(r); got != want {
				t.Fatalf("duplicate key of %q: %q, former %q", r.ForthPN, got, want)
			}
			if got, want := sap.rows.test(envOf(r, empty)), formerSAPEligible(r); got != want {
				t.Fatalf("SAP rows %+v: %v, former %v", r, got, want)
			}
		}
		if len(eligible) == 0 {
			continue
		}
		sort.SliceStable(eligible, func(i, j int) bool { return eligible[i].OriginalIndex < eligible[j].OriginalIndex })
		gotMain, gotReason := c.chooseMain(eligible, empty)
		wantMain, wantReason := formerChooseMain(eligible)
		if gotMain != wantMain || gotReason != wantReason {
			t.Fatalf("group %d: MAIN %+v %q, former %+v %q", n, gotMain, gotReason, wantMain, wantReason)
		}
	}
}

func TestSAPFieldsMatchFormerGoRules(t *testing.T) {
	st := defaultSAP(t)
	for i, rec := range []map[string]any{
		{"mmsta": "Z1", "vname": " Acme ", "lifnr": "100", "labst": 5.0},
		{"mmsta": "", "mstae": "z1", "lifnr": "100", "labst": nil, "stock": "7"},
		{"status": "OK", "vname": 0.0, "labst": 0.0, "stock": 9.0},
		{"zblock": "B1", "stock": 3.0},
		{},
	} {
		got, want := &Row{ForthPN: createPN}, &Row{ForthPN: createPN}
		st.apply(got, rec)
		formerApplySAP(want, rec)
		if fmt.Sprint(*got) != fmt.Sprint(*want) {
			t.Errorf("record %d: %+v, former %+v", i, *got, *want)
		}
	}
}

func formerApplySAP(r *Row, rec map[string]any) {
	status := ""
	if v := firstTruthy(rec, "mmsta", "mstae", "zblock", "status"); v != nil {
		status = jsx.Trim(jsx.String(v))
	}
	if status != "" {
		r.BlockStatus = status
	}
	if jsx.Norm(r.BlockStatus) == "Z1" || jsx.Norm(status) == "Z1" {
		r.BlockStatus, r.IsZ1 = "Z1", true
		if r.ForthPN == createPN {
			r.ForthPN = ""
		}
	}
	vname, lifnr := "", ""
	if v := rec["vname"]; jsx.Truthy(v) {
		vname = jsx.Trim(jsx.String(v))
	}
	if v := rec["lifnr"]; jsx.Truthy(v) {
		lifnr = jsx.Trim(jsx.String(v))
	}
	r.VName, r.VendorCode, r.Vendor = vname, lifnr, vname
	if r.Vendor == "" {
		r.Vendor = lifnr
	}
	stockVal, has := rec["labst"]
	if !has || stockVal == nil {
		stockVal = rec["stock"]
	}
	r.Stock = toFloatAny(stockVal)
}

func TestMaterialMasterRowsMatchFormerGoRules(t *testing.T) {
	m := defaultMaster(t)
	rng := rand.New(rand.NewSource(2))
	for n := 0; n < 500; n++ {
		r := randomRow(rng, n)
		r.EffectiveDescription = []string{"PCB, MAIN", "CAP", ""}[rng.Intn(3)]
		if got, want := m.rows.test(envOf(r, nil)), r.InScope && !r.IsLevel0 && r.HasIJK; got != want {
			t.Fatalf("rows %+v: %v, former %v", r, got, want)
		}
		if got, want := m.retry.test(envOf(r, nil)), isPcbRow(r); got != want {
			t.Fatalf("retry %+v: %v, former %v", r, got, want)
		}
	}
	if m.retrySuffix != "_SANMINA" {
		t.Errorf("suffix %q", m.retrySuffix)
	}
}

func defaultSAP(t *testing.T) *sapStep {
	var spec SAPSpec
	bomStep(t, "sap_enrich", &spec)
	return must[*sapStep](t)(spec.compile())
}

func TestSpecsAreChecked(t *testing.T) {
	for name, err := range map[string]error{
		"query without @mpns": func() error { _, err := MasterSpec{Rows: "true", Query: "SELECT 1"}.compile(); return err }(),
		"retry without suffix": func() error {
			s := MasterSpec{Rows: "true", Query: "IN (@mpns)"}
			s.Retry = &struct {
				Rows   string `yaml:"rows"`
				Suffix string `yaml:"suffix"`
			}{Rows: "pcb"}
			_, err := s.compile()
			return err
		}(),
		"unknown row field": func() error { _, err := MasterSpec{Rows: "nope", Query: "@mpns"}.compile(); return err }(),
		"no employee id":    func() error { _, err := SAPSpec{Rows: "true"}.compile(); return err }(),
		"no main rule":      func() error { _, err := SelectSpec{DuplicateKey: `""`}.compile(); return err }(),
		"bad when": func() error {
			var s SelectSpec
			s.DuplicateKey = `""`
			s.Main = append(s.Main, struct {
				Rows       string `yaml:"rows"`
				When       string `yaml:"when"`
				Pick       string `yaml:"pick"`
				Reason     string `yaml:"reason"`
				ReasonTied string `yaml:"reason_tied"`
			}{Rows: "true", When: "level0", Reason: "x"})
			_, err := s.compile()
			return err
		}(),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
