package bom

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	"miservice/internal/jsx"
)

// rowEnv is what every BOM rule expression in a workflow file can read about
// a row. Text fields are as stored unless noted.
type rowEnv struct {
	Brand     string  `expr:"brand"`     // manufacturer, trimmed upper case
	MPN       string  `expr:"mpn"`       // MPN, trimmed upper case, leading punctuation removed
	Desc      string  `expr:"desc"`      // effective description, trimmed upper case
	InScope   bool    `expr:"in_scope"`  // above the last PCB row
	Level0    bool    `expr:"level0"`    // the assembly (level 0) row
	HasIJK    bool    `expr:"has_ijk"`   // has manufacturer name / MPN data
	PCB       bool    `expr:"pcb"`       // the row (or its group) describes a PCB
	Z1        bool    `expr:"z1"`        // blocked (Z1) by Material_Master or SAP
	Forbidden bool    `expr:"forbidden"` // CFT-001 verdict
	Exempt    bool    `expr:"exempt"`    // allowed by a vendor_policy exception
	ForthPN   string  `expr:"forth_pn"`  // Forth P/N ("CREATE P/N" when none exists yet)
	Priority  any     `expr:"priority"`  // value from the Priority workbook, or nil
	Vendor    string  `expr:"vendor"`    // SAP vendor name, else vendor code, trimmed
	Stock     float64 `expr:"stock"`     // SAP stock
}

// envOf builds the expression environment of r. policy may be nil (then
// exempt is false), as it is while the vendor policy itself is evaluated.
func envOf(r *Row, policy *VendorPolicy) rowEnv {
	d := r.EffectiveDescription
	if d == "" {
		d = r.Description
	}
	e := rowEnv{
		Brand: jsx.Norm(r.MfgName), MPN: stripLeadingNonAlnum(jsx.Norm(r.MfrPN)), Desc: jsx.Norm(d),
		InScope: r.InScope, Level0: r.IsLevel0, HasIJK: r.HasIJK, PCB: isPcbRow(r), Z1: r.IsZ1, Forbidden: r.IsForbidden,
		ForthPN: r.ForthPN, Stock: r.Stock,
	}
	if r.PriorityValue != nil {
		e.Priority = *r.PriorityValue
	}
	if e.Vendor = jsx.Trim(r.VName); e.Vendor == "" {
		e.Vendor = jsx.Trim(r.VendorCode)
	}
	if policy != nil {
		e.Exempt = IsExemptUnderPolicy(r, policy)
	}
	return e
}

// rule is a compiled row condition.
type rule struct{ prog *vm.Program }

// norm is available in every rule: trimmed upper case, as BOM matching does.
var norm = expr.Function("norm", func(p ...any) (any, error) { return jsx.Norm(p[0].(string)), nil }, new(func(string) string))

func compileRule(src string) (rule, error) {
	if strings.TrimSpace(src) == "" {
		return rule{}, fmt.Errorf("condition must not be blank")
	}
	p, err := expr.Compile(src, expr.Env(rowEnv{}), norm, expr.AsBool())
	if err != nil {
		return rule{}, fmt.Errorf("%q: %w", src, err)
	}
	return rule{p}, nil
}

// test evaluates the rule. Compile typed the environment and the result,
// so evaluation cannot fail.
func (c rule) test(env rowEnv) bool {
	v, err := expr.Run(c.prog, env)
	return err == nil && v.(bool)
}

// textRule is a compiled row expression that yields text.
type textRule struct{ prog *vm.Program }

func compileText(src string) (textRule, error) {
	p, err := expr.Compile(src, expr.Env(rowEnv{}), norm, expr.AsKind(reflect.String))
	if err != nil {
		return textRule{}, fmt.Errorf("%q: %w", src, err)
	}
	return textRule{p}, nil
}

func (c textRule) text(env rowEnv) string {
	v, err := expr.Run(c.prog, env)
	if err != nil {
		return ""
	}
	return v.(string)
}

func stripLeadingNonAlnum(s string) string {
	return strings.TrimLeftFunc(s, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	})
}
