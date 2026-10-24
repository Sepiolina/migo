package bom

import (
	"regexp"
	"strings"

	"miservice/internal/jsx"
)

// DefaultVendorPolicy is rules/vendorPolicy.js, and THE file to edit when the
// business changes the CFT-001 rules:
//
//   - ban a vendor outright: add its (upper-case) name to BannedUnconditional;
//   - ban a brand except for some parts: add a BrandException, built from the
//     small matchers/conditions below. The engine (vendorpolicy.go) and the
//     log lines pick it up with no other change.
var DefaultVendorPolicy = &VendorPolicy{
	BannedUnconditional: []string{"YAGEO CORPORATION", "WALSIN TECHNOLOGY CORPORATION", "KDS"},
	BrandExceptions: []BrandException{
		{
			Name:       "TDK",
			LogLabel:   "TDK Exception",
			LogSuffix:  "TDK CAP (CGA series) row(s) confirmed valid.",
			BrandMatch: brandIsTDK,
			// Non-CAP TDK parts are always exempt; CAP parts need a CGA MPN.
			IsExempt: capsMustSatisfy(mpnStartsWith("CGA")),
		},
		{
			Name:       "SAMSUNG",
			LogLabel:   "Samsung Exception",
			LogSuffix:  "Samsung MLCC (CL...P...) row(s) confirmed valid.",
			BrandMatch: brandContainsAny("SAMSUNG", "SEMCO"),
			// Non-CAP parts are always exempt; CAP parts must be automotive MLCC.
			IsExempt: capsMustSatisfy(func(r *Row) bool { return hasAutomotivePAt9or13(r.MfrPN) }),
		},
	},
}

// ---- building blocks for brand exceptions ---------------------------------

var tdkWordRe = regexp.MustCompile(`\bTDK\b`)

// brandIsTDK: "TDK" as a word or as the start of the name ("TDK CORP"), but
// not buried inside another word.
func brandIsTDK(brand string) bool {
	b := jsx.Norm(brand)
	return b != "" && strings.Contains(b, "TDK") && (tdkWordRe.MatchString(b) || strings.HasPrefix(b, "TDK"))
}

// brandContainsAny matches a brand containing any of the (upper-case) tokens.
func brandContainsAny(tokens ...string) func(string) bool {
	return func(brand string) bool {
		b := jsx.Norm(brand)
		if b == "" {
			return false
		}
		for _, t := range tokens {
			if strings.Contains(b, t) {
				return true
			}
		}
		return false
	}
}

// capsMustSatisfy builds an IsExempt condition for brands that are only
// restricted for capacitors: any row whose description does not start with
// "CAP" is exempt, a capacitor row is exempt only if ok(row) holds.
func capsMustSatisfy(ok func(*Row) bool) func(*Row) bool {
	return func(r *Row) bool {
		if !descriptionStartsWithCap(r) {
			return true
		}
		return ok(r)
	}
}

// mpnStartsWith: the MPN, ignoring leading punctuation, starts with prefix.
func mpnStartsWith(prefix string) func(*Row) bool {
	return func(r *Row) bool {
		return strings.HasPrefix(stripLeadingNonAlnum(jsx.Norm(r.MfrPN)), prefix)
	}
}

func stripLeadingNonAlnum(s string) string {
	return strings.TrimLeftFunc(s, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	})
}

func hasAutomotivePAt9or13(mpn string) bool {
	clean := []rune(stripLeadingNonAlnum(jsx.Norm(mpn)))
	if len(clean) < 2 || clean[0] != 'C' || clean[1] != 'L' {
		return false
	}
	return (len(clean) >= 13 && clean[12] == 'P') || (len(clean) >= 9 && clean[8] == 'P')
}

func descriptionStartsWithCap(r *Row) bool {
	d := r.EffectiveDescription
	if d == "" {
		d = r.Description
	}
	return strings.HasPrefix(jsx.Norm(d), "CAP")
}
