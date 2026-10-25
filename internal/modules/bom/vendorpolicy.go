package bom

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	"miservice/internal/jsx"
	"miservice/internal/logx"
)

// VendorPolicySpec is the vendor_policy step argument in a workflow file:
//
//	banned: [YAGEO CORPORATION, KDS]
//	exceptions:
//	  - name: TDK
//	    brand: 'brand startsWith "TDK"'          # which rows the rule covers
//	    allow: 'desc not startsWith "CAP" || ...'   # true = the row is allowed
//	    log: "TDK Exception: {count} row(s) confirmed valid."
//
// Expressions see brand (normalised manufacturer), mpn (normalised MPN
// without leading punctuation) and desc (normalised effective description).
type VendorPolicySpec struct {
	Banned     []string          `yaml:"banned"`
	Exceptions []VendorException `yaml:"exceptions"`
}

// VendorException is one brand rule of a VendorPolicySpec.
type VendorException struct {
	Name  string `yaml:"name"`
	Brand string `yaml:"brand"` // which rows the rule covers
	Allow string `yaml:"allow"` // true = the row is allowed after all
	Log   string `yaml:"log"`
}

// policyEnv is what vendor policy expressions can read.
type policyEnv struct {
	Brand string `expr:"brand"`
	MPN   string `expr:"mpn"`
	Desc  string `expr:"desc"`
}

// BrandException is a brand that is banned unless a per-row condition holds.
type BrandException struct {
	Name  string // key in the result counts
	Log   string // logged when count > 0; "{count}" is replaced
	brand *vm.Program
	allow *vm.Program
}

// VendorPolicy is the compiled CFT-001 policy.
type VendorPolicy struct {
	BannedUnconditional []string
	BrandExceptions     []BrandException
}

// Compile checks every expression and builds the policy.
func (s VendorPolicySpec) Compile() (*VendorPolicy, error) {
	p := &VendorPolicy{BannedUnconditional: s.Banned}
	for i, e := range s.Exceptions {
		if strings.TrimSpace(e.Name) == "" || e.Brand == "" || e.Allow == "" {
			return nil, fmt.Errorf("exception %d: needs name, brand and allow", i+1)
		}
		ex := BrandException{Name: e.Name, Log: e.Log}
		var err error
		if ex.brand, err = expr.Compile(e.Brand, expr.Env(policyEnv{}), expr.AsBool()); err != nil {
			return nil, fmt.Errorf("exception %s brand: %w", e.Name, err)
		}
		if ex.allow, err = expr.Compile(e.Allow, expr.Env(policyEnv{}), expr.AsBool()); err != nil {
			return nil, fmt.Errorf("exception %s allow: %w", e.Name, err)
		}
		p.BrandExceptions = append(p.BrandExceptions, ex)
	}
	return p, nil
}

func envOf(r *Row) policyEnv {
	d := r.EffectiveDescription
	if d == "" {
		d = r.Description
	}
	return policyEnv{Brand: jsx.Norm(r.MfgName), MPN: stripLeadingNonAlnum(jsx.Norm(r.MfrPN)), Desc: jsx.Norm(d)}
}

// test runs a compiled boolean expression. Compile typed the environment and
// the result, so evaluation cannot fail.
func test(p *vm.Program, env policyEnv) bool {
	v, err := expr.Run(p, env)
	return err == nil && v.(bool)
}

func stripLeadingNonAlnum(s string) string {
	return strings.TrimLeftFunc(s, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	})
}

// VendorPolicyResult is what runVendorPolicy returns for logging.
type VendorPolicyResult struct {
	NForbidden  int
	ExemptCount map[string]int
}

func (p *VendorPolicy) matchingException(env policyEnv) *BrandException {
	for i := range p.BrandExceptions {
		if test(p.BrandExceptions[i].brand, env) {
			return &p.BrandExceptions[i]
		}
	}
	return nil
}

// RunVendorPolicy ports ruleEngine.runVendorPolicy (sets Row.IsForbidden).
func RunVendorPolicy(rows []*Row, p *VendorPolicy) VendorPolicyResult {
	banned := map[string]bool{}
	for _, b := range p.BannedUnconditional {
		banned[jsx.Norm(b)] = true
	}
	res := VendorPolicyResult{ExemptCount: map[string]int{}}
	for _, e := range p.BrandExceptions {
		res.ExemptCount[e.Name] = 0
	}
	for _, r := range rows {
		if !r.InScope {
			r.IsForbidden = false
			continue
		}
		env := envOf(r)
		if rule := p.matchingException(env); rule != nil {
			exempt := test(rule.allow, env)
			r.IsForbidden = !exempt
			if exempt {
				res.ExemptCount[rule.Name]++
			}
		} else {
			r.IsForbidden = banned[jsx.Norm(r.MfgName)]
		}
		if r.IsForbidden {
			res.NForbidden++
		}
	}
	return res
}

// IsExemptUnderPolicy ports ruleEngine.isExemptUnderPolicy.
func IsExemptUnderPolicy(r *Row, p *VendorPolicy) bool {
	env := envOf(r)
	if rule := p.matchingException(env); rule != nil {
		return test(rule.allow, env)
	}
	return false
}

// LogVendorPolicyResult ports ruleEngine.logVendorPolicyResult.
func LogVendorPolicyResult(res VendorPolicyResult, p *VendorPolicy, log logx.Logger) {
	for _, rule := range p.BrandExceptions {
		if c := res.ExemptCount[rule.Name]; c > 0 && rule.Log != "" {
			log.Print(strings.ReplaceAll(rule.Log, "{count}", strconv.Itoa(c)))
		}
	}
	log.Printf("CFT-001: %d row(s) flagged as forbidden.", res.NForbidden)
}
