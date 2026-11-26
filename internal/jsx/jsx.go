// Package jsx holds small helpers that reproduce the exact JavaScript
// semantics the Node version of miservice relied on (String.prototype.trim,
// Number#toString, parseFloat, ...). The migration requirement is cell-for-cell
// identical output, and several of these differ subtly from Go's stdlib
// (e.g. strings.TrimSpace does not trim U+FEFF, strconv does not switch to
// exponent form at 1e21 the way JS does).
package jsx

import (
	"math"
	"strconv"
	"strings"
)

// IsSpace reports whether r is JavaScript whitespace (the set used by
// String.prototype.trim and the regexp \s class).
func IsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00A0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// Trim is String.prototype.trim.
func Trim(s string) string { return strings.TrimFunc(s, IsSpace) }

// Norm is helpers.norm(): trim + upper-case.
func Norm(s string) string { return strings.ToUpper(Trim(s)) }

// NumToString is Number.prototype.toString() for float64 (radix 10).
func NumToString(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		return "0" // also covers -0, like JS
	}
	abs := math.Abs(f)
	if abs >= 1e21 || abs < 1e-6 {
		s := strconv.FormatFloat(f, 'e', -1, 64) // e.g. 1e+21, 1.5e-07
		mant, exp, _ := strings.Cut(s, "e")
		sign := exp[:1]
		digits := strings.TrimLeft(exp[1:], "0")
		return mant + "e" + sign + digits
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// ParseFloat is the global parseFloat(): it parses the longest numeric
// prefix and ignores trailing garbage. ok is false where JS returns NaN.
func ParseFloat(s string) (v float64, ok bool) {
	s = strings.TrimLeftFunc(s, IsSpace)
	i := 0
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	if strings.HasPrefix(s[i:], "Infinity") {
		if neg {
			return math.Inf(-1), true
		}
		return math.Inf(1), true
	}
	j := i
	for j < len(s) && isDigit(s[j]) {
		j++
	}
	nInt := j - i
	nFrac := 0
	if j < len(s) && s[j] == '.' {
		k := j + 1
		for k < len(s) && isDigit(s[k]) {
			k++
		}
		nFrac = k - (j + 1)
		if nInt > 0 || nFrac > 0 {
			j = k
		}
	}
	if nInt == 0 && nFrac == 0 {
		return math.NaN(), false
	}
	if j < len(s) && (s[j] == 'e' || s[j] == 'E') {
		k := j + 1
		if k < len(s) && (s[k] == '+' || s[k] == '-') {
			k++
		}
		ds := k
		for k < len(s) && isDigit(s[k]) {
			k++
		}
		if k > ds {
			j = k
		}
	}
	f, _ := strconv.ParseFloat(s[:j], 64) // range errors yield +-Inf, same as JS
	return f, true
}

// ToFloat is helpers.toFloat() for string input: commas stripped, parseFloat,
// default on NaN.
func ToFloat(s string, def float64) float64 {
	cleaned := strings.ReplaceAll(Trim(s), ",", "")
	if v, ok := ParseFloat(cleaned); ok {
		return v
	}
	return def
}
