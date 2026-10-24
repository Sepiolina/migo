package bom

import (
	"math"

	"miservice/internal/jsx"
)

var negInf = math.Inf(-1)

// ProcessGrouping ports processGrouping: forward fill, BOM.Find Num grouping,
// ref-des fill within a group, display find num and the qty zero-fix.
func ProcessGrouping(rows []*Row) {
	if len(rows) > 0 && jsx.Trim(rows[0].Level) == "0" {
		rows[0].IsLevel0 = true
	}
	forwardFillAndGroup(rows)
	fillRefDesWithinGroup(rows)
	setDisplayFindNums(rows)
	fixZeroQtyPerGroup(rows)
}

// forwardFillAndGroup does two interleaved jobs per row, in file order:
// the A-G columns are carried down into the "Effective*" fields, and the row
// is assigned a GroupID (a new group starts when the find number changes).
func forwardFillAndGroup(rows []*Row) {
	var cur struct{ findNum, level, partNumber, partType, description, rev, qty, uom string }
	groupID := 0
	// prevGroupKey in JS is null, a find-num string, or (in the no-find-num
	// branch) a number; a string never equals a number, so a tagged union is
	// enough to reproduce its comparisons.
	prevNull, prevIsStr, prevStr := true, false, ""

	for _, r := range rows {
		r.HasABCDEFG = jsx.Trim(r.Level) != "" || jsx.Trim(r.PartNumber) != "" || jsx.Trim(r.PartType) != "" ||
			jsx.Trim(r.Description) != "" || jsx.Trim(r.Rev) != "" || jsx.Trim(r.Qty) != "" || jsx.Trim(r.UOM) != ""
		r.HasIJK = jsx.Trim(r.RefDes) != "" || jsx.Trim(r.MfgName) != "" || jsx.Trim(r.MfrPN) != ""

		if r.HasABCDEFG {
			set := func(dst *string, v string) {
				if t := jsx.Trim(v); t != "" {
					*dst = t
				}
			}
			set(&cur.findNum, r.FindNum)
			set(&cur.level, r.Level)
			set(&cur.partNumber, r.PartNumber)
			set(&cur.partType, r.PartType)
			set(&cur.description, r.Description)
			set(&cur.rev, r.Rev)
			set(&cur.qty, r.Qty)
			set(&cur.uom, r.UOM)
		}
		if t := jsx.Trim(r.FindNum); t != "" {
			cur.findNum = t
		}

		own := func(own, fill string) string {
			if t := jsx.Trim(own); t != "" {
				return t
			}
			return fill
		}
		r.EffectiveLevel = own(r.Level, cur.level)
		r.EffectivePartNumber = own(r.PartNumber, cur.partNumber)
		r.EffectivePartType = own(r.PartType, cur.partType)
		r.EffectiveDescription = own(r.Description, cur.description)
		r.EffectiveRev = own(r.Rev, cur.rev)
		r.EffectiveQty = own(r.Qty, cur.qty)
		r.EffectiveUOM = own(r.UOM, cur.uom)
		r.EffectiveFindNum = own(r.FindNum, cur.findNum)
		r.GroupFindNum = r.EffectiveFindNum

		if !r.InScope {
			continue
		}
		// Mirrors mimi1.py's forward_fill_and_group, incl. the "prevGroupKey
		// is still null" fallback.
		if cur.findNum != "" {
			key := cur.findNum
			if prevNull || !prevIsStr || key != prevStr {
				groupID++
			}
			prevNull, prevIsStr, prevStr = false, true, key
		} else {
			if r.HasABCDEFG || prevNull {
				groupID++
			}
			prevNull, prevIsStr, prevStr = false, false, ""
		}
		r.GroupID = groupID
	}
}

// fillRefDesWithinGroup is column I (ref des): a blank one is resolved from
// the first ref des of the same BOM.Find Num group.
func fillRefDesWithinGroup(rows []*Row) {
	refDesByGroup := map[string]string{}
	for _, r := range rows {
		if t := jsx.Trim(r.RefDes); t != "" {
			if _, seen := refDesByGroup[r.GroupFindNum]; !seen {
				refDesByGroup[r.GroupFindNum] = t
			}
		}
	}
	for _, r := range rows {
		if t := jsx.Trim(r.RefDes); t != "" {
			r.EffectiveRefDes = t
		} else {
			r.EffectiveRefDes = refDesByGroup[r.GroupFindNum]
		}
	}
}

// setDisplayFindNums decides which rows show the find number in the output
// (the first row of a group, or any row starting a new A-G block).
func setDisplayFindNums(rows []*Row) {
	for i, r := range rows {
		if r.InScope && (i == 0 || rows[i-1].GroupID != r.GroupID || r.HasABCDEFG) {
			if t := jsx.Trim(r.FindNum); t != "" {
				r.DisplayFindNum = t
			} else {
				r.DisplayFindNum = r.GroupFindNum
			}
		} else if r.InScope {
			r.DisplayFindNum = r.EffectiveFindNum
		}
	}
}

// fixZeroQtyPerGroup is the column F (qty) zero-fix, per BOM.Find Num group:
// a zero quantity becomes the group's largest, or "1" if there is none.
func fixZeroQtyPerGroup(rows []*Row) {
	groups := map[string][]*Row{}
	for _, r := range rows {
		groups[r.GroupFindNum] = append(groups[r.GroupFindNum], r)
	}
	for _, grp := range groups {
		var maxRow *Row
		maxVal := negInf
		for _, r := range grp {
			if v := jsx.ToFloat(r.EffectiveQty, 0); v > maxVal {
				maxVal, maxRow = v, r
			}
		}
		replacement := "1"
		if maxVal > 0 {
			replacement = maxRow.EffectiveQty
		}
		for _, r := range grp {
			if jsx.ToFloat(r.EffectiveQty, 0) == 0 {
				r.EffectiveQty = replacement
			}
		}
	}
}

// markScope flags the rows after the LAST PCB row as out of scope (they are
// only copied to the output). Without any PCB row everything is in scope.
func markScope(rows []*Row) {
	lastPcb := -1
	for i, r := range rows {
		if isPcbRow(r) {
			lastPcb = i
		}
	}
	for i, r := range rows {
		r.InScope = lastPcb == -1 || i <= lastPcb
	}
}
