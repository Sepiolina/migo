package bom

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	"miservice/internal/jsx"
	"miservice/internal/logx"
)

// SelectSpec is the select_priority (CFT-002) step argument:
//
//	ineligible:        # first match wins; reason goes to the debug sheet
//	  - {when: level0, reason: Ineligible (Level 0)}
//	duplicate_key: 'norm(forth_pn)'   # later rows with the same key are excluded ("" = never)
//	main:              # first rule whose rows match picks MAIN
//	  - rows: 'priority == 1'          # candidates this rule considers
//	    when: 'count == 1'             # default 'count > 0'; also stock, tied
//	    pick: top                      # top (first row) or most_stock (ties: first row)
//	    reason: MAIN (Cond A: Single P1 match)
//	    reason_tied: ...               # used instead when several rows share the top stock
//
// Reasons may use {stock}, {tied}, {count} and {vendor} (of the chosen row).
// The other candidates become SUB1, SUB2... in file order.
type SelectSpec struct {
	Ineligible []struct {
		When   string `yaml:"when"`
		Reason string `yaml:"reason"`
	} `yaml:"ineligible"`
	DuplicateKey string `yaml:"duplicate_key"`
	Main         []struct {
		Rows       string `yaml:"rows"`
		When       string `yaml:"when"`
		Pick       string `yaml:"pick"`
		Reason     string `yaml:"reason"`
		ReasonTied string `yaml:"reason_tied"`
	} `yaml:"main"`
}

// Cft002 is a compiled SelectSpec.
type Cft002 struct {
	ineligible []ineligibleRule
	dupKey     textRule
	main       []mainRule
}

type ineligibleRule struct {
	when   rule
	reason string
}

type mainRule struct {
	rows               rule
	when               *vm.Program
	mostStock          bool
	reason, reasonTied string
}

// groupEnv is what a main rule's when sees about the rows it matched.
type groupEnv struct {
	Count int     `expr:"count"` // rows matched
	Stock float64 `expr:"stock"` // highest stock among them
	Tied  int     `expr:"tied"`  // rows sharing that stock
}

func (s SelectSpec) compile() (*Cft002, error) {
	c := &Cft002{}
	for i, in := range s.Ineligible {
		r, err := compileRule(in.When)
		if err != nil {
			return nil, fmt.Errorf("ineligible %d: %w", i+1, err)
		}
		if strings.TrimSpace(in.Reason) == "" {
			return nil, fmt.Errorf("ineligible %d: needs a reason", i+1)
		}
		c.ineligible = append(c.ineligible, ineligibleRule{r, in.Reason})
	}
	var err error
	if c.dupKey, err = compileText(s.DuplicateKey); err != nil {
		return nil, fmt.Errorf("duplicate_key: %w", err)
	}
	if len(s.Main) == 0 {
		return nil, fmt.Errorf("main needs at least one rule")
	}
	for i, m := range s.Main {
		mr := mainRule{reason: m.Reason, reasonTied: m.ReasonTied}
		if mr.rows, err = compileRule(m.Rows); err != nil {
			return nil, fmt.Errorf("main %d rows: %w", i+1, err)
		}
		when := m.When
		if when == "" {
			when = "count > 0"
		}
		if mr.when, err = expr.Compile(when, expr.Env(groupEnv{}), expr.AsBool()); err != nil {
			return nil, fmt.Errorf("main %d when: %q: %w", i+1, when, err)
		}
		switch m.Pick {
		case "", "top":
		case "most_stock":
			mr.mostStock = true
		default:
			return nil, fmt.Errorf("main %d: pick must be top or most_stock", i+1)
		}
		if strings.TrimSpace(m.Reason) == "" {
			return nil, fmt.Errorf("main %d: needs a reason", i+1)
		}
		c.main = append(c.main, mr)
	}
	return c, nil
}

// RunCft002 ports runCft002: MAIN / SUBn selection per BOM group.
//
// For every group of rows (one BOM.Find Num) it decides which eligible row is
// the MAIN source and numbers the rest SUB1, SUB2... It also fills the Debug
// columns that explain each decision.
func RunCft002(rows []*Row, pmap PriorityMap, c *Cft002, policy *VendorPolicy, log logx.Logger) {
	prepareDebugColumns(rows, pmap)

	groups, ids := inScopeGroups(rows)
	nMain := 0
	for _, gid := range ids {
		if c.selectMainAndSubs(groups[gid], policy) {
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
func (c *Cft002) selectMainAndSubs(grp []*Row, policy *VendorPolicy) bool {
	c.markEligibility(grp, policy)
	eligible := filterRows(grp, func(r *Row) bool { return r.DebugEligible == "Eligible" })
	if len(eligible) == 0 {
		for _, r := range grp {
			r.DebugReason = r.DebugEligible
			r.Priority = ""
		}
		return false
	}

	c.flagDuplicates(eligible, policy)
	candidates := filterRows(eligible, func(r *Row) bool { return !r.IsDuplicateForthPN })
	if len(candidates) == 0 {
		for _, r := range eligible {
			r.Priority = ""
			r.DebugReason = "Duplicate Forth P/N"
		}
		return false
	}

	main, reason := c.chooseMain(candidates, policy)
	if main == nil {
		for _, r := range grp {
			r.Priority = ""
			r.DebugReason = r.DebugEligible
		}
		return false
	}
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

// markEligibility sets DebugEligible: the first matching ineligible rule
// names why a row cannot be MAIN/SUB.
func (c *Cft002) markEligibility(grp []*Row, policy *VendorPolicy) {
	for _, r := range grp {
		r.DebugEligible = "Eligible"
		env := envOf(r, policy)
		for _, in := range c.ineligible {
			if in.when.test(env) {
				r.DebugEligible = in.reason
				break
			}
		}
	}
}

// flagDuplicates: inside the group the top-most (lowest original index) row
// of a duplicate key is real; later ones are flagged and excluded from
// MAIN/SUB before selection. A blank key is never a duplicate.
func (c *Cft002) flagDuplicates(eligible []*Row, policy *VendorPolicy) {
	byIdx := append([]*Row(nil), eligible...)
	sort.SliceStable(byIdx, func(i, j int) bool { return byIdx[i].OriginalIndex < byIdx[j].OriginalIndex })
	seen := map[string]bool{}
	for _, r := range byIdx {
		r.IsDuplicateForthPN = false
		k := c.dupKey.text(envOf(r, policy))
		if k == "" {
			continue
		}
		if seen[k] {
			r.IsDuplicateForthPN = true
		} else {
			seen[k] = true
		}
	}
}

// chooseMain applies the main rules in order: the first whose matched rows
// pass its when picks MAIN. Ties are broken by the lowest original index
// ("top-most"). It returns nil when no rule applies.
func (c *Cft002) chooseMain(candidates []*Row, policy *VendorPolicy) (*Row, string) {
	for _, m := range c.main {
		var rows []*Row
		for _, r := range candidates {
			if m.rows.test(envOf(r, policy)) {
				rows = append(rows, r)
			}
		}
		g := groupEnv{Count: len(rows)}
		var top []*Row
		if len(rows) > 0 {
			g.Stock = maxStock(rows)
			top = withStock(rows, g.Stock)
			g.Tied = len(top)
		}
		if ok, err := expr.Run(m.when, g); err != nil || !ok.(bool) || len(rows) == 0 {
			continue
		}
		main := topMost(rows)
		if m.mostStock {
			main = topMost(top)
		}
		reason := m.reason
		if m.reasonTied != "" && g.Tied > 1 {
			reason = m.reasonTied
		}
		return main, strings.NewReplacer(
			"{stock}", jsx.NumToString(g.Stock), "{tied}", strconv.Itoa(g.Tied),
			"{count}", strconv.Itoa(g.Count), "{vendor}", vendorLabel(main),
		).Replace(reason)
	}
	return nil, ""
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
