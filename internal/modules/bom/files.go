package bom

import (
	"strings"

	"miservice/internal/jsx"
)

// Uploaded names a (path, original filename) pair.
type Uploaded struct{ Path, Name string }

// IdentifyUploadedFiles ports helpers.identifyUploadedFiles: the Priority file
// is recognised by filename; otherwise upload order (BOM first) is assumed.
func IdentifyUploadedFiles(a, b Uploaded) (bom, priority Uploaded) {
	na, nb := strings.ToLower(a.Name), strings.ToLower(b.Name)
	if (strings.Contains(na, "prio") || strings.Contains(na, "shortage")) &&
		(strings.Contains(nb, "bom") || strings.Contains(nb, ".xls")) {
		return b, a
	}
	return a, b
}

// ParseEmployeeNumber ports helpers.parseEmployeeNumber; ok is false for
// missing/invalid values (letters, digits, '-' and '_' only, 1-20 chars).
func ParseEmployeeNumber(raw string) (string, bool) {
	v := cellToStr(raw)
	if len(v) < 1 || len(v) > 20 {
		return "", false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return "", false
		}
	}
	return v, true
}

// BuildOutputFilename ports helpers.buildOutputFilename:
// OriginalFileName_employeeNumber.xlsx
func BuildOutputFilename(originalName, employeeNumber string) string {
	base := originalName
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	// path.parse(x).name: strip the last extension, but not for ".hidden"
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	var b strings.Builder
	for _, r := range base {
		switch {
		case r == '\\' || r == '/' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' || r < 0x20:
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	name := jsx.Trim(b.String())
	if name == "" {
		name = "output"
	}
	return name + "_" + employeeNumber + ".xlsx"
}
