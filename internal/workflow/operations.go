package workflow

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"miservice/internal/jsx"
)

// CloneTable copies all mutable table data, including style settings.
func CloneTable(t *Table) *Table {
	if t == nil {
		return nil
	}
	out := &Table{Style: cloneStyle(t.Style)}
	if t.Columns != nil {
		out.Columns = append([]string{}, t.Columns...)
	}
	if t.Rows != nil {
		out.Rows = make([][]string, len(t.Rows))
	}
	for i, r := range t.Rows {
		if r != nil {
			out.Rows[i] = append([]string{}, r...)
		}
	}
	return out
}
func cloneStyle(s *StyleSpec) *StyleSpec {
	if s == nil {
		return nil
	}
	c := *s
	if s.Header != nil {
		h := *s.Header
		c.Header = &h
	}
	if s.NumberFormats != nil {
		c.NumberFormats = map[string]string{}
		for k, v := range s.NumberFormats {
			c.NumberFormats[k] = v
		}
	}
	return &c
}

// ValidateTable checks the unique, nonblank headers and rectangular rows.
func ValidateTable(t *Table) error {
	if t == nil {
		return fmt.Errorf("table is nil")
	}
	seen := map[string]bool{}
	for _, c := range t.Columns {
		if strings.TrimSpace(c) == "" {
			return fmt.Errorf("blank column header")
		}
		if seen[c] {
			return fmt.Errorf("duplicate column header %q", c)
		}
		seen[c] = true
	}
	for i, r := range t.Rows {
		if len(r) != len(t.Columns) {
			return fmt.Errorf("row %d: got %d cells, want %d", i+1, len(r), len(t.Columns))
		}
	}
	return nil
}
func col(t *Table, ref string) (int, error) {
	if strings.HasPrefix(ref, "@") {
		n := 0
		for _, c := range ref[1:] {
			if c < 'A' || c > 'Z' {
				return 0, fmt.Errorf("unknown column %q", ref)
			}
			if n > (len(t.Columns)+26)/26 {
				return 0, fmt.Errorf("unknown column %q", ref)
			}
			n = n*26 + int(c-'A'+1)
		}
		if n > 0 && n <= len(t.Columns) {
			return n - 1, nil
		}
		return 0, fmt.Errorf("unknown column %q", ref)
	}
	for i, c := range t.Columns {
		if c == ref {
			return i, nil
		}
	}
	return 0, fmt.Errorf("unknown column %q", ref)
}
func cols(t *Table, refs []string) ([]int, error) {
	out := make([]int, len(refs))
	seen := map[int]bool{}
	for i, r := range refs {
		n, e := col(t, r)
		if e != nil {
			return nil, e
		}
		if seen[n] {
			return nil, fmt.Errorf("duplicate column reference %q", r)
		}
		seen[n] = true
		out[i] = n
	}
	return out, nil
}

// numeric deliberately rejects blank cells as well as NaN and infinities.
func numeric(s string, row int, column string) (float64, error) {
	v, e := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if e != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("row %d column %q: invalid finite number %q", row+1, column, s)
	}
	return v, nil
}
func number(v float64) (string, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "", fmt.Errorf("non-finite arithmetic result")
	}
	return strconv.FormatFloat(v, 'g', -1, 64), nil
}
func key(row []string, indices []int, normalize bool) string {
	v := make([]string, len(indices))
	for i, n := range indices {
		v[i] = row[n]
		if normalize {
			v[i] = jsx.Norm(v[i])
		}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// ApplyStep returns an independent result and never mutates either input source.
func ApplyStep(table *Table, step Step, datasets map[string]*Table) (*Table, error) {
	if e := ValidateTable(table); e != nil {
		return nil, e
	}
	count := 0
	for _, ok := range []bool{step.Map != nil, step.Filter != nil, step.ForwardFill != nil, step.Group != nil, step.Aggregate != nil, step.Sort != nil, step.Deduplicate != nil, step.Calculate != nil, step.Transform != nil, step.Join != nil, step.Merge != nil, step.Compare != nil, step.Style != nil} {
		if ok {
			count++
		}
	}
	if count != 1 {
		return nil, fmt.Errorf("step requires exactly one operation, got %d", count)
	}
	t := CloneTable(table)
	if step.Enabled != nil && !*step.Enabled {
		return t, nil
	}
	var e error
	switch {
	case step.Map != nil:
		t, e = mapTable(t, *step.Map)
	case step.Filter != nil:
		var predicate func([]string, int) (bool, error)
		predicate, e = compileFilter(t, *step.Filter)
		if e == nil {
			rows := t.Rows
			t.Rows = nil
			for i, r := range rows {
				var yes bool
				yes, e = predicate(r, i)
				if e != nil {
					break
				}
				if yes {
					t.Rows = append(t.Rows, r)
				}
			}
		}
	case step.ForwardFill != nil:
		var ix []int
		ix, e = cols(t, step.ForwardFill.Columns)
		if e == nil {
			last := make([]string, len(ix))
			for _, r := range t.Rows {
				for j, n := range ix {
					if strings.TrimSpace(r[n]) == "" {
						r[n] = last[j]
					} else {
						last[j] = r[n]
					}
				}
			}
		}
	case step.Group != nil:
		t, e = groupTable(t, step.Group.By, step.Group.Aggregate, false)
	case step.Aggregate != nil:
		t, e = groupTable(t, nil, step.Aggregate.Columns, true)
	case step.Sort != nil:
		e = sortTable(t, *step.Sort)
	case step.Deduplicate != nil:
		var ix []int
		ix, e = cols(t, step.Deduplicate.Columns)
		if e == nil {
			seen := map[string]bool{}
			rows := t.Rows
			t.Rows = nil
			for _, r := range rows {
				k := key(r, ix, false)
				if !seen[k] {
					seen[k] = true
					t.Rows = append(t.Rows, r)
				}
			}
		}
	case step.Calculate != nil:
		e = calculate(t, *step.Calculate)
	case step.Transform != nil:
		e = transform(t, *step.Transform)
	case step.Join != nil:
		t, e = joinTable(t, *step.Join, datasets)
	case step.Merge != nil:
		e = mergeTables(t, *step.Merge, datasets)
	case step.Compare != nil:
		t, e = compareTables(t, *step.Compare, datasets)
	case step.Style != nil:
		style := cloneStyle(step.Style)
		if step.Style.NumberFormats != nil {
			style.NumberFormats = make(map[string]string, len(step.Style.NumberFormats))
		}
		for ref := range step.Style.NumberFormats {
			var n int
			if n, e = col(t, ref); e != nil {
				break
			}
			if _, exists := style.NumberFormats[t.Columns[n]]; exists {
				e = fmt.Errorf("duplicate number format column %q", ref)
				break
			}
			style.NumberFormats[t.Columns[n]] = step.Style.NumberFormats[ref]
		}
		if e == nil {
			t.Style = style
		}
	}
	if e != nil {
		return nil, e
	}
	return t, ValidateTable(t)
}
func mapTable(t *Table, s MapSpec) (*Table, error) {
	src := make([]int, len(s.Columns))
	dest := map[string]bool{}
	remove := map[int]bool{}
	for i, m := range s.Columns {
		n, e := col(t, m.From)
		if e != nil {
			return nil, e
		}
		src[i] = n
		if strings.TrimSpace(m.To) == "" {
			return nil, fmt.Errorf("blank mapping destination")
		}
		if dest[m.To] {
			return nil, fmt.Errorf("duplicate mapping destination %q", m.To)
		}
		dest[m.To] = true
		if s.Keep && m.Move && m.To != t.Columns[n] {
			remove[n] = true
		}
	}
	out := &Table{Style: t.Style}
	indices := []int{}
	if s.Keep {
		for i, c := range t.Columns {
			if !remove[i] {
				out.Columns = append(out.Columns, c)
				indices = append(indices, i)
			}
		}
	}
	for i, m := range s.Columns {
		existing := -1
		for j, c := range out.Columns {
			if c == m.To {
				existing = j
				break
			}
		}
		if existing >= 0 {
			if indices[existing] != src[i] {
				return nil, fmt.Errorf("mapping destination %q clobbers existing column", m.To)
			}
			continue
		}
		out.Columns = append(out.Columns, m.To)
		indices = append(indices, src[i])
	}
	for _, r := range t.Rows {
		nr := make([]string, len(indices))
		for j, n := range indices {
			nr[j] = r[n]
		}
		out.Rows = append(out.Rows, nr)
	}
	out.Style = projectedStyle(t, indices, out.Columns)
	return out, nil
}

// projectedStyle follows copied or renamed columns and removes formats for
// columns that no longer exist. Positional references resolve before projection.
func projectedStyle(t *Table, indices []int, names []string) *StyleSpec {
	s := cloneStyle(t.Style)
	if s == nil || s.NumberFormats == nil {
		return s
	}
	s.NumberFormats = map[string]string{}
	for ref, format := range t.Style.NumberFormats {
		n, err := col(t, ref)
		if err != nil {
			continue
		}
		for i, source := range indices {
			if source == n {
				s.NumberFormats[names[i]] = format
			}
		}
	}
	return s
}
func compileFilter(t *Table, s FilterSpec) (func([]string, int) (bool, error), error) {
	childrenAll := []func([]string, int) (bool, error){}
	childrenAny := []func([]string, int) (bool, error){}
	for _, c := range s.All {
		p, e := compileFilter(t, c)
		if e != nil {
			return nil, e
		}
		childrenAll = append(childrenAll, p)
	}
	for _, c := range s.Any {
		p, e := compileFilter(t, c)
		if e != nil {
			return nil, e
		}
		childrenAny = append(childrenAny, p)
	}
	hasLeaf := s.Column != "" || s.Equal != nil || s.NotEqual != nil || s.GreaterThan != nil || s.GreaterOrEqual != nil || s.LessThan != nil || s.LessOrEqual != nil || s.Contains != nil || s.In != nil || s.Blank != nil
	ix := 0
	var e error
	if hasLeaf {
		ix, e = col(t, s.Column)
		if e != nil {
			return nil, e
		}
	}
	if !hasLeaf && len(childrenAll)+len(childrenAny) == 0 {
		return nil, fmt.Errorf("empty filter")
	}
	for _, v := range []*float64{s.GreaterThan, s.GreaterOrEqual, s.LessThan, s.LessOrEqual} {
		if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0)) {
			return nil, fmt.Errorf("filter threshold must be finite")
		}
	}
	return func(r []string, row int) (bool, error) {
		yes := true
		for _, p := range childrenAll {
			v, e := p(r, row)
			if e != nil {
				return false, e
			}
			yes = yes && v
		}
		if len(childrenAny) > 0 {
			any := false
			for _, p := range childrenAny {
				v, e := p(r, row)
				if e != nil {
					return false, e
				}
				any = any || v
			}
			yes = yes && any
		}
		if !hasLeaf {
			return yes, nil
		}
		v := r[ix]
		if s.Equal != nil {
			yes = yes && v == *s.Equal
		}
		if s.NotEqual != nil {
			yes = yes && v != *s.NotEqual
		}
		if s.Contains != nil {
			yes = yes && strings.Contains(v, *s.Contains)
		}
		if s.In != nil {
			found := false
			for _, x := range s.In {
				found = found || v == x
			}
			yes = yes && found
		}
		if s.Blank != nil {
			yes = yes && ((strings.TrimSpace(v) == "") == *s.Blank)
		}
		if s.GreaterThan != nil || s.GreaterOrEqual != nil || s.LessThan != nil || s.LessOrEqual != nil {
			n, e := numeric(v, row, t.Columns[ix])
			if e != nil {
				return false, e
			}
			if s.GreaterThan != nil {
				yes = yes && n > *s.GreaterThan
			}
			if s.GreaterOrEqual != nil {
				yes = yes && n >= *s.GreaterOrEqual
			}
			if s.LessThan != nil {
				yes = yes && n < *s.LessThan
			}
			if s.LessOrEqual != nil {
				yes = yes && n <= *s.LessOrEqual
			}
		}
		return yes, nil
	}, nil
}
