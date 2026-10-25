package workflow

import (
	"fmt"
	"sort"
	"strings"

	"miservice/internal/jsx"
)

func groupTable(t *Table, by []string, aggregates map[string]string, global bool) (*Table, error) {
	keys, e := cols(t, by)
	if e != nil {
		return nil, e
	}
	keyset := map[int]bool{}
	out := &Table{Style: t.Style}
	for _, n := range keys {
		keyset[n] = true
		out.Columns = append(out.Columns, t.Columns[n])
	}
	ops := map[int]string{}
	for ref, op := range aggregates {
		n, e := col(t, ref)
		if e != nil {
			return nil, e
		}
		if keyset[n] {
			return nil, fmt.Errorf("aggregate column %q is also a group key", ref)
		}
		if _, ok := ops[n]; ok {
			return nil, fmt.Errorf("duplicate aggregate column %q", ref)
		}
		switch op {
		case "sum", "first", "last", "min", "max", "count", "mean", "join":
		default:
			return nil, fmt.Errorf("unknown aggregate %q", op)
		}
		ops[n] = op
	}
	indices := []int{}
	for i, c := range t.Columns {
		if _, ok := ops[i]; ok {
			indices = append(indices, i)
			out.Columns = append(out.Columns, c)
		}
	}
	out.Style = projectedStyle(t, append(append([]int(nil), keys...), indices...), out.Columns)
	groups := [][]int{}
	lookup := map[string]int{}
	if global {
		groups = append(groups, nil)
		lookup["[]"] = 0
	}
	for i, r := range t.Rows {
		k := key(r, keys, false)
		g, ok := lookup[k]
		if !ok {
			g = len(groups)
			lookup[k] = g
			groups = append(groups, nil)
		}
		groups[g] = append(groups[g], i)
	}
	for _, group := range groups {
		row := make([]string, 0, len(out.Columns))
		for _, n := range keys {
			row = append(row, t.Rows[group[0]][n])
		}
		for _, n := range indices {
			v, e := aggregateCell(t, group, n, ops[n])
			if e != nil {
				return nil, e
			}
			row = append(row, v)
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}
func aggregateCell(t *Table, rows []int, n int, op string) (string, error) {
	if op == "count" {
		return number(float64(len(rows)))
	}
	if len(rows) == 0 {
		if op == "sum" {
			return "0", nil
		}
		return "", nil
	}
	switch op {
	case "first":
		return t.Rows[rows[0]][n], nil
	case "last":
		return t.Rows[rows[len(rows)-1]][n], nil
	case "join":
		seen := map[string]bool{}
		values := []string{}
		for _, r := range rows {
			v := t.Rows[r][n]
			if strings.TrimSpace(v) != "" && !seen[v] {
				seen[v] = true
				values = append(values, v)
			}
		}
		return strings.Join(values, ", "), nil
	}
	v := 0.0
	for i, r := range rows {
		x, e := numeric(t.Rows[r][n], r, t.Columns[n])
		if e != nil {
			return "", e
		}
		switch op {
		case "sum", "mean":
			v += x
		case "min":
			if i == 0 || x < v {
				v = x
			}
		case "max":
			if i == 0 || x > v {
				v = x
			}
		}
	}
	if op == "mean" {
		v /= float64(len(rows))
	}
	result, err := number(v)
	if err != nil {
		return "", fmt.Errorf("row %d column %q: %w", rows[len(rows)-1]+1, t.Columns[n], err)
	}
	return result, nil
}
func sortTable(t *Table, s SortSpec) error {
	ix, e := cols(t, s.By)
	if e != nil {
		return e
	}
	if s.Order != "" && s.Order != "ascending" && s.Order != "descending" {
		return fmt.Errorf("unknown sort order %q", s.Order)
	}
	// Pre-parse every numeric cell so errors do not depend on comparator calls.
	type sortable struct {
		row  []string
		nums []float64
	}
	rows := make([]sortable, len(t.Rows))
	for i, r := range t.Rows {
		rows[i].row = r
		if s.Numeric {
			for _, n := range ix {
				v, e := numeric(r[n], i, t.Columns[n])
				if e != nil {
					return e
				}
				rows[i].nums = append(rows[i].nums, v)
			}
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		for k, n := range ix {
			cmp := 0
			if s.Numeric {
				if rows[i].nums[k] < rows[j].nums[k] {
					cmp = -1
				} else if rows[i].nums[k] > rows[j].nums[k] {
					cmp = 1
				}
			} else {
				cmp = strings.Compare(rows[i].row[n], rows[j].row[n])
			}
			if cmp != 0 {
				if s.Order == "descending" {
					return cmp > 0
				}
				return cmp < 0
			}
		}
		return false
	})
	for i, r := range rows {
		t.Rows[i] = r.row
	}
	return nil
}
func calculate(t *Table, s CalculateSpec) error {
	ix, e := cols(t, s.Columns)
	if e != nil {
		return e
	}
	switch s.Operation {
	case "add", "subtract", "multiply", "divide":
	default:
		return fmt.Errorf("unknown calculation %q", s.Operation)
	}
	if len(ix) == 0 && s.Value == nil {
		return fmt.Errorf("calculation requires operands")
	}
	if strings.TrimSpace(s.Column) == "" {
		return fmt.Errorf("blank calculation destination")
	}
	if s.Value != nil {
		if _, e := number(*s.Value); e != nil {
			return fmt.Errorf("calculation value: %w", e)
		}
	}
	dest := -1
	for i, c := range t.Columns {
		if c == s.Column {
			dest = i
		}
	}
	if dest < 0 {
		dest = len(t.Columns)
		t.Columns = append(t.Columns, s.Column)
		for i := range t.Rows {
			t.Rows[i] = append(t.Rows[i], "")
		}
	}
	for i, r := range t.Rows {
		values := []float64{}
		for _, n := range ix {
			v, e := numeric(r[n], i, t.Columns[n])
			if e != nil {
				return e
			}
			values = append(values, v)
		}
		if s.Value != nil {
			values = append(values, *s.Value)
		}
		v := values[0]
		for _, x := range values[1:] {
			switch s.Operation {
			case "add":
				v += x
			case "subtract":
				v -= x
			case "multiply":
				v *= x
			case "divide":
				if x == 0 {
					return fmt.Errorf("row %d column %q: division by zero", i+1, s.Column)
				}
				v /= x
			}
		}
		r[dest], e = number(v)
		if e != nil {
			return fmt.Errorf("row %d column %q: %w", i+1, s.Column, e)
		}
	}
	return nil
}
func transform(t *Table, s TransformSpec) error {
	ix, e := cols(t, s.Columns)
	if e != nil {
		return e
	}
	var f func(string) string
	switch s.Operation {
	case "trim":
		f = strings.TrimSpace
	case "upper":
		f = strings.ToUpper
	case "lower":
		f = strings.ToLower
	case "normalize":
		f = jsx.Norm
	case "replace":
		f = func(v string) string { return strings.ReplaceAll(v, s.Old, s.New) }
	default:
		return fmt.Errorf("unknown transform %q", s.Operation)
	}
	for _, r := range t.Rows {
		for _, n := range ix {
			r[n] = f(r[n])
		}
	}
	return nil
}
