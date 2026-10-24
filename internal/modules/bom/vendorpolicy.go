package bom

import (
	"miservice/internal/jsx"
	"miservice/internal/logx"
)

// BrandException is a brand that is banned unless a per-row condition holds
// (rules/vendorPolicy.js brandExceptions entries).
type BrandException struct {
	Name       string // key in the result counts
	LogLabel   string // "<LogLabel>: <n> <LogSuffix>" is logged when n > 0
	LogSuffix  string
	BrandMatch func(brand string) bool // brand arrives already normalised
	IsExempt   func(r *Row) bool       // true = this row is allowed after all
}

// VendorPolicy is the declarative CFT-001 policy (WHAT is forbidden lives in
// vendorpolicy_default.go; this file is the engine that applies it).
type VendorPolicy struct {
	BannedUnconditional []string
	BrandExceptions     []BrandException
}

// VendorPolicyResult is what runVendorPolicy returns for logging.
type VendorPolicyResult struct {
	NForbidden  int
	ExemptCount map[string]int
}

func (p *VendorPolicy) matchingException(r *Row) *BrandException {
	brand := jsx.Norm(r.MfgName)
	for i := range p.BrandExceptions {
		if p.BrandExceptions[i].BrandMatch(brand) {
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
		if rule := p.matchingException(r); rule != nil {
			exempt := rule.IsExempt(r)
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
	if rule := p.matchingException(r); rule != nil {
		return rule.IsExempt(r)
	}
	return false
}

// LogVendorPolicyResult ports ruleEngine.logVendorPolicyResult.
func LogVendorPolicyResult(res VendorPolicyResult, p *VendorPolicy, log logx.Logger) {
	for _, rule := range p.BrandExceptions {
		if c := res.ExemptCount[rule.Name]; c > 0 {
			log.Printf("%s: %d %s", rule.LogLabel, c, rule.LogSuffix)
		}
	}
	log.Printf("CFT-001: %d row(s) flagged as forbidden.", res.NForbidden)
}
