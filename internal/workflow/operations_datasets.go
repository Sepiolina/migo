package workflow

import "fmt"

func dataset(d map[string]*Table, name string) (*Table, error) {
	t, ok := d[name]
	if !ok {
		return nil, fmt.Errorf("unknown dataset %q", name)
	}
	if e := ValidateTable(t); e != nil {
		return nil, fmt.Errorf("dataset %q: %w", name, e)
	}
	return t, nil
}
func joinTable(t *Table, s JoinSpec, d map[string]*Table) (*Table, error) {
	right, e := dataset(d, s.With)
	if e != nil {
		return nil, e
	}
	leftKeys, e := cols(t, s.On)
	if e != nil {
		return nil, e
	}
	refs := s.RightOn
	if len(refs) == 0 {
		refs = s.On
	}
	rightKeys, e := cols(right, refs)
	if e != nil {
		return nil, e
	}
	if len(leftKeys) == 0 || len(leftKeys) != len(rightKeys) {
		return nil, fmt.Errorf("join requires equal nonempty key lists")
	}
	if s.How != "" && s.How != "left" && s.How != "inner" {
		return nil, fmt.Errorf("unknown join kind %q", s.How)
	}
	suffix := s.Suffix
	if suffix == "" {
		suffix = "_right"
	}
	out := &Table{Columns: append([]string(nil), t.Columns...), Style: t.Style}
	skip := map[int]bool{}
	for _, n := range rightKeys {
		skip[n] = true
	}
	names := map[string]bool{}
	leftNames := map[string]bool{}
	for _, c := range out.Columns {
		names[c] = true
		leftNames[c] = true
	}
	extra := []int{}
	for n, c := range right.Columns {
		if skip[n] {
			continue
		}
		name := c
		if leftNames[name] {
			name += suffix
		}
		if names[name] {
			return nil, fmt.Errorf("join output column collision %q", name)
		}
		names[name] = true
		out.Columns = append(out.Columns, name)
		extra = append(extra, n)
	}
	lookup := map[string][][]string{}
	for _, r := range right.Rows {
		k := key(r, rightKeys, s.Normalize)
		lookup[k] = append(lookup[k], r)
	}
	for _, r := range t.Rows {
		matches := lookup[key(r, leftKeys, s.Normalize)]
		if len(matches) == 0 && s.How != "inner" {
			out.Rows = append(out.Rows, append(append([]string(nil), r...), make([]string, len(extra))...))
		}
		for _, rr := range matches {
			nr := append([]string(nil), r...)
			for _, n := range extra {
				nr = append(nr, rr[n])
			}
			out.Rows = append(out.Rows, nr)
		}
	}
	return out, nil
}
func mergeTables(t *Table, s MergeSpec, d map[string]*Table) error {
	for _, name := range s.With {
		r, e := dataset(d, name)
		if e != nil {
			return e
		}
		if len(r.Columns) != len(t.Columns) {
			return fmt.Errorf("merge dataset %q has different columns", name)
		}
		ix, e := cols(r, t.Columns)
		if e != nil {
			return fmt.Errorf("merge dataset %q: %w", name, e)
		}
		for _, row := range r.Rows {
			nr := make([]string, len(ix))
			for j, n := range ix {
				nr[j] = row[n]
			}
			t.Rows = append(t.Rows, nr)
		}
	}
	return nil
}
func compareTables(t *Table, s CompareSpec, d map[string]*Table) (*Table, error) {
	right, e := dataset(d, s.With)
	if e != nil {
		return nil, e
	}
	lk, e := cols(t, s.By)
	if e != nil {
		return nil, e
	}
	rk, e := cols(right, s.By)
	if e != nil {
		return nil, e
	}
	if len(lk) == 0 {
		return nil, fmt.Errorf("compare requires keys")
	}
	refs := s.Columns
	if len(refs) == 0 {
		keys := map[string]bool{}
		for _, n := range lk {
			keys[t.Columns[n]] = true
		}
		for _, n := range rk {
			keys[right.Columns[n]] = true
		}
		seen := map[string]bool{}
		for _, c := range append(append([]string(nil), t.Columns...), right.Columns...) {
			if !keys[c] && !seen[c] {
				refs = append(refs, c)
				seen[c] = true
			}
		}
	}
	lc, rc := make([]int, len(refs)), make([]int, len(refs))
	seenRefs := map[string]bool{}
	for i, ref := range refs {
		lc[i], e = col(t, ref)
		if e != nil {
			if len(s.Columns) > 0 {
				return nil, e
			}
			lc[i] = -1
		}
		rc[i], e = col(right, ref)
		if e != nil {
			if len(s.Columns) > 0 {
				return nil, e
			}
			rc[i] = -1
		}
		canonical := ref
		if lc[i] >= 0 {
			canonical = t.Columns[lc[i]]
		}
		if seenRefs[canonical] {
			return nil, fmt.Errorf("duplicate compare column %q", ref)
		}
		seenRefs[canonical] = true
	}
	status := s.StatusColumn
	if status == "" {
		status = "status"
	}
	out := &Table{Columns: append([]string(nil), t.Columns...), Style: t.Style}
	names := map[string]int{}
	for i, c := range out.Columns {
		names[c] = i
	}
	for _, c := range right.Columns {
		if _, ok := names[c]; !ok {
			names[c] = len(out.Columns)
			out.Columns = append(out.Columns, c)
		}
	}
	if _, ok := names[status]; ok {
		return nil, fmt.Errorf("compare status column %q already exists", status)
	}
	out.Columns = append(out.Columns, status)
	lookup := map[string][]string{}
	for _, r := range right.Rows {
		k := key(r, rk, s.Normalize)
		if _, ok := lookup[k]; ok {
			return nil, fmt.Errorf("duplicate compare key in dataset %q: %s", s.With, k)
		}
		lookup[k] = r
	}
	seen := map[string]bool{}
	appendRow := func(r []string, columns []string, st string) {
		nr := make([]string, len(out.Columns))
		for i, c := range columns {
			nr[names[c]] = r[i]
		}
		nr[len(nr)-1] = st
		out.Rows = append(out.Rows, nr)
	}
	for _, r := range t.Rows {
		k := key(r, lk, s.Normalize)
		if seen[k] {
			return nil, fmt.Errorf("duplicate compare key in current table: %s", k)
		}
		seen[k] = true
		st := "added"
		if rr, ok := lookup[k]; ok {
			st = "unchanged"
			for i, n := range lc {
				left, baseline := "", ""
				if n >= 0 {
					left = r[n]
				}
				if rc[i] >= 0 {
					baseline = rr[rc[i]]
				}
				if left != baseline {
					st = "changed"
					break
				}
			}
		}
		appendRow(r, t.Columns, st)
	}
	for _, r := range right.Rows {
		if !seen[key(r, rk, s.Normalize)] {
			appendRow(r, right.Columns, "removed")
		}
	}
	return out, nil
}
