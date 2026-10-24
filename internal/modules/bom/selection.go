package bom

import (
	"fmt"
	"sort"

	"miservice/internal/jsx"
	"miservice/internal/logx"
)

// RunCft002 ports runCft002: MAIN / SUBn selection per BOM group.
//
// For every group of rows (one BOM.Find Num) it decides which eligible row is
// the MAIN source and numbers the rest SUB1, SUB2... It also fills the Debug
// columns that explain each decision.
func RunCft002(rows []*Row, pmap PriorityMap, policy *VendorPolicy, log logx.Logger) {
	prepareDebugColumns(rows, pmap)

	groups, ids := inScopeGroups(rows)
	nMain := 0
	for _, gid := range ids {
		if selectMainAndSubs(groups[gid], policy) {
			nMain++
		}
	}
	log.Printf("CFT-002: assigned MAIN to %d group(s).", nMain)
}

// prepareDebugColumns resets per-row state and fills the debug values that do
// not depend on the group decision (out-of-scope rows are finished here).
func prepareDebugColumns(rows []*Row, pmap PriorityMap) {
	for _, r := range rows {
		r.IsDuplicateForthPN = false
		if !r.InScope {
			r.PriorityValue = nil
			r.Priority = ""
			r.DebugP1 = "-"
			r.DebugVName = "-"
			r.DebugVendorCode = "-"
			r.DebugStockQty = DebugStock{Str: "-"}
			r.DebugForthPN = r.ForthPN
			r.DebugEligible = "Ineligible (Below PCB Scope)"
			r.DebugReason = "Out of Scope"
			r.DebugGroupID = "-"
			continue
		}
		pv, has := pmap[jsx.Norm(r.ForthPN)]
		if has && pv != nil {
			v := *pv
			r.PriorityValue = &v
		} else {
			r.PriorityValue = nil
		}
		switch {
		case r.PriorityValue != nil && *r.PriorityValue == 1:
			r.DebugP1 = "P1 (1.0)"
		case r.PriorityValue != nil:
			r.DebugP1 = "Val (" + jsx.NumToString(*r.PriorityValue) + ")"
		default:
			r.DebugP1 = "None"
		}
		vName, vCode := jsx.Trim(r.VName), jsx.Trim(r.VendorCode)
		r.DebugVName = vName
		if vName == "" {
			r.DebugVName = "No Vendor"
		}
		r.DebugVendorCode = vCode
		if vCode == "" {
			r.DebugVendorCode = "-"
		}
		r.DebugStockQty = DebugStock{IsNumber: true, Num: r.Stock}
		r.DebugForthPN = r.ForthPN
		r.DebugGroupID = fmt.Sprint(r.GroupID)
	}
}

// inScopeGroups buckets the in-scope rows by GroupID; ids is sorted.
func inScopeGroups(rows []*Row) (map[int][]*Row, []int) {
	groups := map[int][]*Row{}
	var ids []int
	for _, r := range rows {
		if !r.InScope {
			continue
		}
		if _, ok := groups[r.GroupID]; !ok {
			ids = append(ids, r.GroupID)
		}
		groups[r.GroupID] = append(groups[r.GroupID], r)
	}
	sort.Ints(ids)
	return groups, ids
}

// selectMainAndSubs decides one group. It reports whether a MAIN was chosen.
func selectMainAndSubs(grp []*Row, policy *VendorPolicy) bool {
	markEligibility(grp, policy)
	eligible := filterRows(grp, func(r *Row) bool { return r.DebugEligible == "Eligible" })
	if len(eligible) == 0 {
		for _, r := range grp {
			r.DebugReason = r.DebugEligible
			r.Priority = ""
		}
		return false
	}

	flagDuplicateForthPNs(eligible)
	candidates := filterRows(eligible, func(r *Row) bool { return !r.IsDuplicateForthPN })
	if len(candidates) == 0 {
		for _, r := range eligible {
			r.Priority = ""
			r.DebugReason = "Duplicate Forth P/N"
		}
		return false
	}

	main, reason := chooseMain(candidates)
	main.Priority = "MAIN"
	main.DebugReason = reason

	numberSubs(candidates, main)
	for _, r := range eligible {
		if r.IsDuplicateForthPN {
			r.Priority = ""
			r.DebugReason = "Duplicate Forth P/N (excluded from MAIN/SUB)"
		}
	}
	for _, r := range grp {
		if r.DebugEligible != "Eligible" {
			r.DebugReason = r.DebugEligible
			r.Priority = ""
		}
	}
	return true
}

// markEligibility sets DebugEligible: the first failing check names why a row
// cannot be MAIN/SUB.
func markEligibility(grp []*Row, policy *VendorPolicy) {
	for _, r := range grp {
		switch {
		case r.IsLevel0:
			r.DebugEligible = "Ineligible (Level 0)"
		case !r.HasIJK:
			r.DebugEligible = "Ineligible (No I,J,K)"
		case r.IsZ1:
			r.DebugEligible = "Ineligible (Z1 Blocked)"
		case r.IsForbidden && !IsExemptUnderPolicy(r, policy):
			r.DebugEligible = "Ineligible (CFT-001 Forbidden)"
		default:
			r.DebugEligible = "Eligible"
		}
	}
}

// flagDuplicateForthPNs: inside the group the top-most (lowest original
// index) occurrence of a Forth P/N is real; later ones are flagged and
// excluded from MAIN/SUB before selection.
func flagDuplicateForthPNs(eligible []*Row) {
	byIdx := append([]*Row(nil), eligible...)
	sort.SliceStable(byIdx, func(i, j int) bool { return byIdx[i].OriginalIndex < byIdx[j].OriginalIndex })
	seen := map[string]bool{}
	for _, r := range byIdx {
		r.IsDuplicateForthPN = false
		fpn := jsx.Norm(r.ForthPN)
		if fpn == "" || fpn == createPN {
			continue
		}
		if seen[fpn] {
			r.IsDuplicateForthPN = true
		} else {
			seen[fpn] = true
		}
	}
}

// chooseMain applies the selection rules in order and returns the winner with
// the explanation shown in the Debug sheet:
//
//	A  exactly one P1 row                          -> that row
//	B  several P1 rows                             -> most stock (vendors preferred)
//	C  no P1 row                                   -> first row with a vendor,
//	                                                  else most stock
//	D  no P1, no vendor, no stock                  -> the top-most row
//
// Ties are always broken by the lowest original index ("top-most").
func chooseMain(candidates []*Row) (*Row, string) {
	p1 := filterRows(candidates, isPriorityOne)
	switch {
	case len(p1) == 1:
		return p1[0], "MAIN (Cond A: Single P1 match)"
	case len(p1) > 1:
		return chooseAmongSeveralP1(p1)
	}
	return chooseWithoutP1(candidates)
}

func chooseAmongSeveralP1(p1 []*Row) (*Row, string) {
	if withVendor := filterRows(p1, hasVendor); len(withVendor) > 0 {
		maxS := maxStock(withVendor)
		cand := withStock(withVendor, maxS)
		main := topMost(cand)
		vd := vendorLabel(main)
		if len(cand) > 1 {
			return main, fmt.Sprintf("MAIN (Cond B: Multiple P1 & Vendors -> Highest Stock: %s [%d tied, top-most: %s])", jsx.NumToString(maxS), len(cand), vd)
		}
		return main, fmt.Sprintf("MAIN (Cond B: Multiple P1 & Vendors -> Highest Stock: %s [%s])", jsx.NumToString(maxS), vd)
	}
	maxS := maxStock(p1)
	cand := withStock(p1, maxS)
	return topMost(cand), fmt.Sprintf("MAIN (Cond B: Multiple P1, No Vendor -> Highest Stock: %s [%d tied, top-most])", jsx.NumToString(maxS), len(cand))
}

func chooseWithoutP1(candidates []*Row) (*Row, string) {
	if withVendor := filterRows(candidates, hasVendor); len(withVendor) > 0 {
		main := topMost(withVendor)
		return main, fmt.Sprintf("MAIN (Cond C: No P1 -> Vendor match [%s, top-most])", vendorLabel(main))
	}
	if maxS := maxStock(candidates); maxS > 0 {
		cand := withStock(candidates, maxS)
		return topMost(cand), fmt.Sprintf("MAIN (Cond C: No P1, No Vendor -> Highest Stock: %s [%d tied, top-most])", jsx.NumToString(maxS), len(cand))
	}
	return topMost(candidates), "MAIN (Cond D: No P1, No Vendor, No Stock -> Fallback top-most row)"
}

// numberSubs labels every other candidate SUB1, SUB2... in file order.
func numberSubs(candidates []*Row, main *Row) {
	subs := filterRows(candidates, func(r *Row) bool { return r != main })
	sort.SliceStable(subs, func(i, j int) bool { return subs[i].OriginalIndex < subs[j].OriginalIndex })
	for i, r := range subs {
		r.Priority = fmt.Sprintf("SUB%d", i+1)
		r.DebugReason = fmt.Sprintf("SUB%d (Alternate in group)", i+1)
	}
}

// ---- small row predicates / reducers ---------------------------------------

func isPriorityOne(r *Row) bool { return r.PriorityValue != nil && *r.PriorityValue == 1 }

func hasVendor(r *Row) bool {
	vn := jsx.Trim(r.VName)
	if vn == "" {
		vn = jsx.Trim(r.VendorCode)
	}
	return vn != "" && vn != "0" && vn != "None" && vn != "-"
}

// vendorLabel is the vendor name shown in a decision reason (name, else code).
func vendorLabel(r *Row) string {
	if r.VName != "" {
		return r.VName
	}
	return r.VendorCode
}

func filterRows(rs []*Row, keep func(*Row) bool) []*Row {
	var out []*Row
	for _, r := range rs {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

// topMost is the row with the lowest original index.
func topMost(rs []*Row) *Row {
	best := rs[0]
	for _, r := range rs[1:] {
		if r.OriginalIndex < best.OriginalIndex {
			best = r
		}
	}
	return best
}

func maxStock(rs []*Row) float64 {
	m := rs[0].Stock
	for _, r := range rs[1:] {
		if r.Stock > m {
			m = r.Stock
		}
	}
	return m
}

func withStock(rs []*Row, stock float64) []*Row {
	return filterRows(rs, func(r *Row) bool { return r.Stock == stock })
}
