package bom

import (
	"strings"

	"miservice/internal/jsx"
)

// buildSheet ports writeSheet's content/formatting logic (row 0 = header).
// debugMode adds the debug block (columns T-AA) and its styling; the same
// rows produce both the "BOM" and the "debug" sheet.
func buildSheet(rows []*Row, debugMode bool, policy *VendorPolicy) *grid {
	g := newGrid()
	addHeaderRow(g, debugMode)
	lastOfGroup := lastRowOfEachGroup(rows)

	for i, r := range rows {
		exempt := IsExemptUnderPolicy(r, policy)
		vs := rowValues(r, debugMode, exempt)
		ri := g.addRow(vs)
		if r.InScope {
			styleScopedRow(g, ri, r, vs, exempt, lastOfGroup[i])
		}
		if debugMode {
			styleDebugBlock(g, ri, vs, r.InScope && lastOfGroup[i])
		}
	}
	return g
}

func addHeaderRow(g *grid, debugMode bool) {
	headers := append([]string(nil), outputHeaders...)
	if debugMode {
		headers = append(headers, debugHeadersTOnward...)
	}
	hv := make([]val, len(headers))
	for i, h := range headers {
		hv[i] = str(h)
	}
	hr := g.addRow(hv)
	for c := range headers {
		switch {
		case debugMode && c >= colDebugP1:
			g.fill(hr, c, debugHeaderFill)
			g.font(hr, c, fontDebugHeader)
		case debugMode && c >= colDebugSpacerFirst && c <= colDebugSpacerLast:
			// spacer columns keep the plain header cell
		default:
			g.font(hr, c, fontHeader)
		}
	}
}

// lastRowOfEachGroup marks the last in-scope row of every group: it gets the
// thick bottom border that visually separates groups.
func lastRowOfEachGroup(rows []*Row) []bool {
	isLast := make([]bool, len(rows))
	for i, r := range rows {
		if !r.InScope {
			continue
		}
		isLast[i] = i+1 >= len(rows) || !rows[i+1].InScope || rows[i+1].GroupID != r.GroupID
	}
	return isLast
}

// ---- cell values -------------------------------------------------------------

// rowValues is the row's cell values; exempt says the vendor policy lets this
// (otherwise forbidden) brand through.
func rowValues(r *Row, debugMode, exempt bool) []val {
	if r.InScope {
		return scopedValues(r, debugMode, exempt)
	}
	return outOfScopeValues(r, debugMode)
}

func scopedValues(r *Row, debugMode, exempt bool) []val {
	blockStatus := r.BlockStatus
	if r.IsZ1 {
		blockStatus = "Z1"
	} else if blockStatus == "FORBIDDEN" {
		blockStatus = ""
	}
	forthPn, priority := r.ForthPN, r.Priority
	if !r.HasIJK || r.IsLevel0 {
		forthPn, priority = "", ""
	}
	if (r.IsForbidden || r.IsZ1) && !exempt && forthPn == createPN {
		forthPn = ""
	}
	vs := []val{
		str(r.EffectiveLevel), str(r.EffectivePartNumber), str(r.EffectivePartType), str(r.EffectiveDescription), str(r.EffectiveRev),
		str(r.EffectiveQty), str(r.EffectiveUOM), str(r.DisplayFindNum), str(r.EffectiveRefDes),
		str(r.MfgName), str(r.MfrPN), str(""),
		str(blockStatus), str(forthPn), str(priority),
	}
	if debugMode {
		ds := str(r.DebugStockQty.Str)
		if r.DebugStockQty.IsNumber {
			ds = val{n: r.DebugStockQty.Num, isNum: true}
		}
		vs = append(vs, str(""), str(""), str(""), str(""),
			str(r.DebugP1), str(r.DebugVName), str(r.DebugVendorCode), ds,
			str(r.DebugForthPN), str(r.DebugEligible), str(r.DebugReason), str(r.DebugGroupID))
	}
	return vs
}

func outOfScopeValues(r *Row, debugMode bool) []val {
	mpn := r.OrigMfrPN
	if mpn == "" {
		mpn = r.MfrPN
	}
	vs := []val{
		str(r.EffectiveLevel), str(r.EffectivePartNumber), str(r.EffectivePartType), str(r.EffectiveDescription), str(r.EffectiveRev),
		str(r.EffectiveQty), str(r.EffectiveUOM), str(r.EffectiveFindNum), str(r.EffectiveRefDes),
		str(r.MfgName), str(mpn), str(""), str(""), str(""), str(""),
	}
	if debugMode {
		vs = append(vs, str(""), str(""), str(""), str(""),
			str("-"), str("-"), str("-"), str("-"),
			str(mpn), str("Ineligible (Below PCB Scope)"), str("Out of Scope"), str("-"))
	}
	return vs
}

// ---- styling -------------------------------------------------------------------

// styleScopedRow applies the colour rules of the BOM columns A-O.
func styleScopedRow(g *grid, ri int, r *Row, vs []val, exempt, lastInGroup bool) {
	g.fill(ri, colSeparator, black)
	if r.IsLevel0 {
		for c := 0; c < nBaseCols; c++ {
			g.fill(ri, c, yellow)
		}
	}
	if vs[colForthPN].s == createPN {
		g.fill(ri, colForthPN, yellow)
	}
	if (r.IsForbidden || r.IsZ1) && !exempt {
		for _, c := range []int{colMfgName, colMfrPN} {
			g.fill(ri, c, red)
			g.font(ri, c, fontWhite)
		}
	}
	pv := strings.ToUpper(jsx.Trim(vs[colPriority].s))
	if pv == "MAIN" {
		g.fill(ri, colPriority, blue)
		g.font(ri, colPriority, fontWhite)
	} else if strings.HasPrefix(pv, "SUB") {
		g.fill(ri, colPriority, green)
		g.font(ri, colPriority, fontWhite)
	}
	if r.IsDuplicateForthPN {
		g.fill(ri, colPriority, black)
		g.font(ri, colPriority, fontWhite)
	}
	if lastInGroup {
		for c := 0; c < nBaseCols; c++ {
			g.border(ri, c, borderSpec{b: thickBlack})
		}
	}
}

// styleDebugBlock styles the debug columns T-AA of one row.
func styleDebugBlock(g *grid, ri int, vs []val, lastInGroup bool) {
	bottom := thinGray
	if lastInGroup {
		bottom = thickBlack
	}
	for c := colDebugP1; c <= colDebugGroupID; c++ {
		left := thinGray
		if c == colDebugP1 {
			left = debugLeft
		}
		g.border(ri, c, borderSpec{l: left, r: thinGray, t: thinGray, b: bottom})
		g.font(ri, c, fontDebugNormal)
		styleDebugCell(g, ri, c, vs[c])
	}
}

// styleDebugCell adds the per-column highlighting on top of the base debug style.
func styleDebugCell(g *grid, ri, c int, v val) {
	vstr := v.text()
	switch c {
	case colDebugP1:
		if strings.Contains(vstr, "P1") {
			g.fill(ri, c, "FFF8E1")
			g.font(ri, c, fontP1Gold)
		} else if vstr == "-" || vstr == "None" || vstr == "" {
			g.font(ri, c, fontDebugDim)
		}
	case colDebugVName:
		if vstr == "No Vendor" || vstr == "-" || vstr == "" {
			g.font(ri, c, fontDebugDim)
		}
	case colDebugVendorCode:
		if vstr == "-" || vstr == "" {
			g.font(ri, c, fontDebugDim)
		}
	case colDebugStock:
		if v.isNum {
			g.thousands(ri, c)
			if v.n > 0 {
				g.font(ri, c, fontStockPos)
			} else {
				g.font(ri, c, fontDebugDim)
			}
		} else {
			g.font(ri, c, fontDebugDim)
		}
	case colDebugForthPN:
		g.font(ri, c, fontDebugBold)
	case colDebugEligible:
		switch {
		case vstr == "Eligible":
			g.fill(ri, c, "E8F5E9")
			g.font(ri, c, fontEligible)
		case strings.Contains(vstr, "Ineligible"):
			if strings.Contains(vstr, "Scope") {
				g.fill(ri, c, "F5F5F5")
				g.font(ri, c, fontOutScope)
			} else {
				g.fill(ri, c, "FFEBEE")
				g.font(ri, c, fontIneligible)
			}
		default:
			g.fill(ri, c, "F5F5F5")
			g.font(ri, c, fontOutScope)
		}
	case colDebugReason:
		switch {
		case strings.HasPrefix(vstr, "MAIN"):
			g.fill(ri, c, "E3F2FD")
			g.font(ri, c, fontReasonMain)
		case strings.HasPrefix(vstr, "SUB"):
			g.fill(ri, c, "F1F8E9")
			g.font(ri, c, fontReasonSub)
		case strings.Contains(vstr, "Ineligible") || strings.Contains(vstr, "Scope"):
			g.font(ri, c, fontOutScope)
		}
	case colDebugGroupID:
		g.font(ri, c, fontDebugBold)
	}
}
