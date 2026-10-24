package bom

import (
	"strings"

	"miservice/internal/jsx"
	"miservice/internal/logx"
)

func isASCIIAlnumUpper(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// startsWithPCBToken is /^PCB(?![A-Z0-9])/.
func startsWithPCBToken(s string) bool {
	if !strings.HasPrefix(s, "PCB") {
		return false
	}
	return len(s) == 3 || !isASCIIAlnumUpper(s[3])
}

// IsPcbItem ports helpers.isPcbItem.
func IsPcbItem(description string) bool {
	d := jsx.Norm(description)
	if d == "" {
		return false
	}
	if startsWithPCBToken(d) {
		return true
	}
	for _, t := range strings.FieldsFunc(d, func(r rune) bool {
		return r == ',' || r == ';' || r == ':' || r == '/' || r == '|' || r == '\\'
	}) {
		t = jsx.Trim(t)
		if t == "PCB" || startsWithPCBToken(t) {
			return true
		}
	}
	return false
}

// isPcbRow: the row's own description or its forward-filled one says PCB.
func isPcbRow(r *Row) bool {
	return IsPcbItem(r.Description) || IsPcbItem(r.EffectiveDescription)
}

// ExtractRevToken ports helpers.extractRevToken (first whitespace-delimited token).
func ExtractRevToken(rev string) string {
	s := cellToStr(rev)
	if s == "" {
		return ""
	}
	if i := strings.IndexFunc(s, jsx.IsSpace); i >= 0 {
		return s[:i]
	}
	return s
}

// ReformatPcbMpn ports helpers.reformatPcbMpn.
func ReformatPcbMpn(origMpn, revToken string) string {
	orig := jsx.Trim(origMpn)
	rev := jsx.Trim(revToken)
	if orig == "" {
		return ""
	}
	if rev == "" || strings.Contains(orig, "_REV_"+rev) {
		return orig
	}
	return orig + "_REV_" + rev
}

const sanminaSuffix = "_SANMINA"

// WithSanminaSuffix: <customer_pn>_REV_<REV> -> ..._SANMINA (idempotent).
func WithSanminaSuffix(mpn string) string {
	m := jsx.Trim(mpn)
	if m == "" {
		return m
	}
	if strings.HasSuffix(jsx.Norm(m), sanminaSuffix) {
		return m
	}
	return m + sanminaSuffix
}

// reformatPcbRows mirrors mimi1.py's apply_pcb_modifications (see the long
// comment in the Node source): a PCB row's MPN becomes <pn>_REV_<rev>, using
// the forward-filled rev/part number as fallback, and HasIJK flips on when
// the reformat gave the row an MPN.
func reformatPcbRows(rows []*Row, log logx.Logger) {
	reformatted := 0
	for _, r := range rows {
		if !r.InScope || !isPcbRow(r) {
			continue
		}
		revSrc := r.Rev
		if revSrc == "" {
			revSrc = r.EffectiveRev
		}
		revToken := ExtractRevToken(revSrc)
		baseMpn := jsx.Trim(r.MfrPN)
		if baseMpn == "" {
			baseMpn = jsx.Trim(r.PartNumber)
			if baseMpn == "" {
				baseMpn = jsx.Trim(r.EffectivePartNumber)
			}
		}
		if baseMpn != "" && revToken != "" {
			if newMpn := ReformatPcbMpn(baseMpn, revToken); newMpn != r.MfrPN {
				r.MfrPN = newMpn
				r.HasIJK = true
				reformatted++
			}
		}
	}
	if reformatted > 0 {
		log.Printf("Reformatted Column K for %d PCB row(s).", reformatted)
	}
}
