package workflow

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"miservice/internal/jsx"
)

// Cells are typed: string, float64, bool or nil (blank). Sheets read as
// text; expressions may produce numbers and booleans; REST and SQL keep the
// types they return.

// cell normalises a value produced by an expression, REST or SQL.
func cell(v any) any {
	switch x := v.(type) {
	case nil, string, float64, bool:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case int32:
		return float64(x)
	case float32:
		return float64(x)
	case []byte:
		return string(x)
	}
	return jsx.String(v)
}

// text is a cell as text: JavaScript String() for numbers, "" for blank.
func text(v any) string {
	switch x := cell(v).(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return jsx.NumToString(x)
	case bool:
		return strconv.FormatBool(x)
	}
	return jsx.String(v)
}

// blank is nil or whitespace-only text.
func blank(v any) bool { return v == nil || jsx.Trim(text(v)) == "" }

// numeric is a cell as a number; blank cells, NaN and infinities are errors.
func numeric(v any, row int, column string) (float64, error) {
	switch x := v.(type) {
	case float64:
		if !math.IsNaN(x) && !math.IsInf(x, 0) {
			return x, nil
		}
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(x), 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f, nil
		}
	}
	return 0, fmt.Errorf("row %d column %q: invalid number %q", row+1, column, text(v))
}

// key joins cells into a comparison key; normalize compares trimmed upper case.
func key(row []any, ix []int, normalize bool) string {
	var b strings.Builder
	for _, n := range ix {
		s := text(row[n])
		if normalize {
			s = jsx.Norm(s)
		}
		b.WriteString(strconv.Quote(s))
		b.WriteByte(0)
	}
	return b.String()
}

func compareCells(a, b any, numericOrder bool) int {
	if numericOrder {
		x, y := toNumber(a, 0), toNumber(b, 0)
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	}
	return strings.Compare(text(a), text(b))
}

// toNumber is lenient: numbers as they are, text by JavaScript parseFloat
// (commas ignored), anything else def.
func toNumber(v any, def float64) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		return jsx.ToFloat(x, def)
	}
	return def
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
