package workflow

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Table is a sheet of typed cells with unique column names.
type Table struct {
	Columns []string
	Rows    [][]any
}

// tableFn transforms the current table; t is a private copy it may modify.
type tableFn func(t *Table, s *State) (*Table, error)

// tableStep wraps a compiler of table functions so every step works on a
// copy (saved datasets never change) and leaves a valid table.
func tableStep(compile func(arg *yaml.Node, sc *scope) (tableFn, error)) compiler {
	return func(arg *yaml.Node, sc *scope) (Runner, error) {
		if arg == nil {
			return nil, fmt.Errorf("needs an argument")
		}
		fn, err := compile(arg, sc)
		if err != nil {
			return nil, err
		}
		return func(_ context.Context, s *State) error {
			cur, err := s.current()
			if err != nil {
				return err
			}
			t, err := fn(cur.clone(), s)
			if err != nil {
				return err
			}
			if err := t.validate(); err != nil {
				return err
			}
			s.Cur = t
			return nil
		}, nil
	}
}

func (s *State) current() (*Table, error) {
	if s.Cur == nil {
		return nil, fmt.Errorf("no current table: start with a 'from' step")
	}
	return s.Cur, nil
}

func (s *State) dataset(name string) (*Table, error) {
	t, ok := s.Tables[name]
	if !ok {
		return nil, fmt.Errorf("unknown dataset %q (save it first)", name)
	}
	return t, nil
}

// - save: name keeps a snapshot of the current table.
func compileSave(arg *yaml.Node, sc *scope) (Runner, error) {
	if arg == nil || arg.Kind != yaml.ScalarNode || strings.TrimSpace(arg.Value) == "" {
		return nil, fmt.Errorf("names the dataset to save")
	}
	name := arg.Value
	if sc.inputs[name] {
		return nil, fmt.Errorf("dataset %q would hide the input of the same name", name)
	}
	sc.saved[name] = true
	return func(_ context.Context, s *State) error {
		cur, err := s.current()
		if err != nil {
			return err
		}
		s.Tables[name] = cur.clone()
		return nil
	}, nil
}

// - select: {New: Source, ...} keeps only these columns, in this order.
func compileSelect(arg *yaml.Node, _ *scope) (tableFn, error) {
	pairs, err := orderedPairs(arg)
	if err != nil {
		return nil, err
	}
	return func(t *Table, _ *State) (*Table, error) {
		src := make([]int, len(pairs))
		out := &Table{}
		for i, p := range pairs {
			n, err := t.col(p[1])
			if err != nil {
				return nil, err
			}
			src[i] = n
			out.Columns = append(out.Columns, p[0])
		}
		for _, r := range t.Rows {
			nr := make([]any, len(src))
			for j, n := range src {
				nr[j] = r[n]
			}
			out.Rows = append(out.Rows, nr)
		}
		return out, nil
	}, nil
}

// - rename: {Old: New, ...} keeps every column.
// - rename: '<expression of name>' renames every column, e.g. 'lower(name)'.
func compileRename(arg *yaml.Node, _ *scope) (tableFn, error) {
	if arg.Kind == yaml.ScalarNode {
		p, err := compileExpr(arg.Value, false, "name")
		if err != nil {
			return nil, err
		}
		return func(t *Table, _ *State) (*Table, error) {
			for i, c := range t.Columns {
				v, err := p.run(map[string]any{"name": c})
				if err != nil {
					return nil, err
				}
				t.Columns[i] = text(cell(v))
			}
			return t, nil
		}, nil
	}
	pairs, err := orderedPairs(arg)
	if err != nil {
		return nil, err
	}
	return func(t *Table, _ *State) (*Table, error) {
		for _, p := range pairs {
			n, err := t.col(p[0])
			if err != nil {
				return nil, err
			}
			t.Columns[n] = p[1]
		}
		return t, nil
	}, nil
}

// - filter: '<row condition>' keeps the rows where it is true.
func compileFilter(arg *yaml.Node, _ *scope) (tableFn, error) {
	p, err := compileExpr(arg.Value, true, rowVars...)
	if err != nil {
		return nil, err
	}
	return func(t *Table, s *State) (*Table, error) {
		if err := p.check(t); err != nil {
			return nil, err
		}
		var keep [][]any
		for i := range t.Rows {
			ok, err := p.test(rowEnv(t, i, s.vars()))
			if err != nil {
				return nil, fmt.Errorf("row %d: %w", i+1, err)
			}
			if ok {
				keep = append(keep, t.Rows[i])
			}
		}
		t.Rows = keep
		return t, nil
	}, nil
}

// - set: {Col: '<expression>', ...} computes columns in order, row by row;
// new columns are appended. prev("Col") is the previous row's value, so
// running values (counters, carried state) are expressions too.
func compileSet(arg *yaml.Node, _ *scope) (tableFn, error) {
	pairs, err := orderedPairs(arg)
	if err != nil {
		return nil, err
	}
	progs := make([]*program, len(pairs))
	for i, p := range pairs {
		if progs[i], err = compileExpr(p[1], true, rowVars...); err != nil {
			return nil, fmt.Errorf("%s: %w", p[0], err)
		}
	}
	return func(t *Table, s *State) (*Table, error) {
		for k, p := range pairs {
			dest := t.addColumn(p[0])
			if err := progs[k].check(t); err != nil {
				return nil, err
			}
			for i := range t.Rows {
				v, err := progs[k].run(rowEnv(t, i, s.vars()))
				if err != nil {
					return nil, fmt.Errorf("row %d column %q: %w", i+1, p[0], err)
				}
				t.Rows[i][dest] = cell(v)
			}
		}
		return t, nil
	}, nil
}

// - fill: [Col, ...] forward-fills blank cells from the row above.
func compileFill(arg *yaml.Node, _ *scope) (tableFn, error) {
	names, err := nameList(arg)
	if err != nil {
		return nil, err
	}
	return func(t *Table, _ *State) (*Table, error) {
		ix, err := t.cols(names)
		if err != nil {
			return nil, err
		}
		last := make([]any, len(ix))
		for _, r := range t.Rows {
			for j, n := range ix {
				if blank(r[n]) {
					if last[j] != nil {
						r[n] = last[j]
					}
				} else {
					last[j] = r[n]
				}
			}
		}
		return t, nil
	}, nil
}

// - dedupe: [Col, ...] keeps the first row of each key.
// - dedupe: {by: [Col, ...], keep: first|last, normalize: false}
func compileDedupe(arg *yaml.Node, _ *scope) (tableFn, error) {
	var spec struct {
		By        []string `yaml:"by"`
		Keep      string   `yaml:"keep"`
		Normalize bool     `yaml:"normalize"`
	}
	if arg.Kind == yaml.SequenceNode {
		if err := strictDecode(arg, &spec.By); err != nil {
			return nil, err
		}
	} else if err := strictDecode(arg, &spec); err != nil {
		return nil, err
	}
	if len(spec.By) == 0 || (spec.Keep != "" && spec.Keep != "first" && spec.Keep != "last") {
		return nil, fmt.Errorf("needs by, and keep first or last")
	}
	return func(t *Table, _ *State) (*Table, error) {
		ix, err := t.cols(spec.By)
		if err != nil {
			return nil, err
		}
		winner := map[string]int{}
		for i, r := range t.Rows {
			k := key(r, ix, spec.Normalize)
			if _, seen := winner[k]; !seen || spec.Keep == "last" {
				winner[k] = i
			}
		}
		var keep [][]any
		for i, r := range t.Rows {
			if winner[key(r, ix, spec.Normalize)] == i {
				keep = append(keep, r)
			}
		}
		t.Rows = keep
		return t, nil
	}, nil
}

// - sort: {by: [Col, ...], desc: false, numeric: false}; stable.
func compileSort(arg *yaml.Node, _ *scope) (tableFn, error) {
	var spec struct {
		By      []string `yaml:"by"`
		Desc    bool     `yaml:"desc"`
		Numeric bool     `yaml:"numeric"`
	}
	if err := strictDecode(arg, &spec); err != nil {
		return nil, err
	}
	if len(spec.By) == 0 {
		return nil, fmt.Errorf("by must name at least one column")
	}
	return func(t *Table, _ *State) (*Table, error) {
		ix, err := t.cols(spec.By)
		if err != nil {
			return nil, err
		}
		if spec.Numeric {
			for i, r := range t.Rows {
				for _, n := range ix {
					if _, err := numeric(r[n], i, t.Columns[n]); err != nil {
						return nil, err
					}
				}
			}
		}
		sort.SliceStable(t.Rows, func(a, b int) bool {
			for _, n := range ix {
				if c := compareCells(t.Rows[a][n], t.Rows[b][n], spec.Numeric); c != 0 {
					return (c < 0) != spec.Desc
				}
			}
			return false
		})
		return t, nil
	}, nil
}

//   - join: {with: dataset, on: [Key], right_on: [Key], how: left|inner,
//     normalize: false, take: {New: RightCol}, suffix: _right}
//
// take picks (and names) the columns to bring over; without it every
// non-key column comes, with suffix on name clashes.
func compileJoin(arg *yaml.Node, sc *scope) (tableFn, error) {
	var spec struct {
		With      string    `yaml:"with"`
		On        []string  `yaml:"on"`
		RightOn   []string  `yaml:"right_on"`
		How       string    `yaml:"how"`
		Normalize bool      `yaml:"normalize"`
		Take      yaml.Node `yaml:"take"`
		Suffix    string    `yaml:"suffix"`
	}
	if err := strictDecode(arg, &spec); err != nil {
		return nil, err
	}
	if spec.With == "" || len(spec.On) == 0 {
		return nil, fmt.Errorf("needs with and on")
	}
	if !sc.saved[spec.With] {
		return nil, fmt.Errorf("unknown dataset %q (save it first)", spec.With)
	}
	if spec.RightOn == nil {
		spec.RightOn = spec.On
	}
	if len(spec.RightOn) != len(spec.On) {
		return nil, fmt.Errorf("on and right_on must have the same length")
	}
	if spec.How != "" && spec.How != "left" && spec.How != "inner" {
		return nil, fmt.Errorf("how must be left or inner")
	}
	if spec.Suffix == "" {
		spec.Suffix = "_right"
	}
	var take [][2]string
	if spec.Take.Kind != 0 {
		var err error
		if take, err = orderedPairs(&spec.Take); err != nil {
			return nil, fmt.Errorf("take: %w", err)
		}
	}
	return func(t *Table, s *State) (*Table, error) {
		right, err := s.dataset(spec.With)
		if err != nil {
			return nil, err
		}
		lk, err := t.cols(spec.On)
		if err != nil {
			return nil, err
		}
		rk, err := right.cols(spec.RightOn)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", spec.With, err)
		}
		out := &Table{Columns: append([]string(nil), t.Columns...)}
		var extra []int
		if take != nil {
			for _, p := range take {
				n, err := right.col(p[1])
				if err != nil {
					return nil, fmt.Errorf("%s: %w", spec.With, err)
				}
				out.Columns = append(out.Columns, p[0])
				extra = append(extra, n)
			}
		} else {
			for n, c := range right.Columns {
				if contains(spec.RightOn, c) {
					continue
				}
				if t.index(c) >= 0 {
					c += spec.Suffix
				}
				out.Columns = append(out.Columns, c)
				extra = append(extra, n)
			}
		}
		lookup := map[string][][]any{}
		for _, r := range right.Rows {
			k := key(r, rk, spec.Normalize)
			lookup[k] = append(lookup[k], r)
		}
		for _, r := range t.Rows {
			matches := lookup[key(r, lk, spec.Normalize)]
			if len(matches) == 0 && spec.How != "inner" {
				out.Rows = append(out.Rows, append(append([]any(nil), r...), make([]any, len(extra))...))
			}
			for _, m := range matches {
				nr := append([]any(nil), r...)
				for _, n := range extra {
					nr = append(nr, m[n])
				}
				out.Rows = append(out.Rows, nr)
			}
		}
		return out, nil
	}, nil
}

// - append: [dataset, ...] adds their rows; columns are matched by name and
// the result has the union of the columns.
func compileAppend(arg *yaml.Node, sc *scope) (tableFn, error) {
	names, err := nameList(arg)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if !sc.saved[name] {
			return nil, fmt.Errorf("unknown dataset %q (save it first)", name)
		}
	}
	return func(t *Table, s *State) (*Table, error) {
		parts := []*Table{t}
		for _, name := range names {
			d, err := s.dataset(name)
			if err != nil {
				return nil, err
			}
			parts = append(parts, d)
		}
		return unionRows(parts...), nil
	}, nil
}

// unionRows stacks tables, columns matched by name (first-seen order).
func unionRows(parts ...*Table) *Table {
	out := &Table{}
	for _, p := range parts {
		for _, c := range p.Columns {
			out.addColumn(c)
		}
	}
	for _, p := range parts {
		ix := make([]int, len(p.Columns))
		for i, c := range p.Columns {
			ix[i] = out.index(c)
		}
		for _, r := range p.Rows {
			nr := make([]any, len(out.Columns))
			for i, n := range ix {
				nr[n] = r[i]
			}
			out.Rows = append(out.Rows, nr)
		}
	}
	return out
}

// - stack: {columns: [A, B], into: Key} turns each row into one row per
// listed column (in order), holding that column's value in Key.
func compileStack(arg *yaml.Node, _ *scope) (tableFn, error) {
	var spec struct {
		Columns []string `yaml:"columns"`
		Into    string   `yaml:"into"`
	}
	if err := strictDecode(arg, &spec); err != nil {
		return nil, err
	}
	if len(spec.Columns) == 0 || spec.Into == "" {
		return nil, fmt.Errorf("needs columns and into")
	}
	return func(t *Table, _ *State) (*Table, error) {
		ix, err := t.cols(spec.Columns)
		if err != nil {
			return nil, err
		}
		out := &Table{}
		var keep []int
		for i, c := range t.Columns {
			if !contains(spec.Columns, c) {
				out.Columns = append(out.Columns, c)
				keep = append(keep, i)
			}
		}
		out.Columns = append(out.Columns, spec.Into)
		for _, r := range t.Rows {
			for _, n := range ix {
				nr := make([]any, 0, len(out.Columns))
				for _, k := range keep {
					nr = append(nr, r[k])
				}
				out.Rows = append(out.Rows, append(nr, r[n]))
			}
		}
		return out, nil
	}, nil
}

// - compare: {with: baseline, by: [Key], columns: [..], normalize: false, status: status}
// adds a status column: added, removed, changed or unchanged.
func compileCompare(arg *yaml.Node, sc *scope) (tableFn, error) {
	var spec struct {
		With      string   `yaml:"with"`
		By        []string `yaml:"by"`
		Columns   []string `yaml:"columns"`
		Normalize bool     `yaml:"normalize"`
		Status    string   `yaml:"status"`
	}
	if err := strictDecode(arg, &spec); err != nil {
		return nil, err
	}
	if spec.With == "" || len(spec.By) == 0 {
		return nil, fmt.Errorf("needs with and by")
	}
	if !sc.saved[spec.With] {
		return nil, fmt.Errorf("unknown dataset %q (save it first)", spec.With)
	}
	if spec.Status == "" {
		spec.Status = "status"
	}
	return func(t *Table, s *State) (*Table, error) {
		base, err := s.dataset(spec.With)
		if err != nil {
			return nil, err
		}
		lk, err := t.cols(spec.By)
		if err != nil {
			return nil, err
		}
		rk, err := base.cols(spec.By)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", spec.With, err)
		}
		compared := spec.Columns
		if compared == nil {
			for _, c := range append(append([]string(nil), t.Columns...), base.Columns...) {
				if !contains(spec.By, c) && !contains(compared, c) {
					compared = append(compared, c)
				}
			}
		}
		out := unionRows(&Table{Columns: t.Columns}, &Table{Columns: base.Columns})
		if out.index(spec.Status) >= 0 {
			return nil, fmt.Errorf("status column %q already exists", spec.Status)
		}
		out.Columns = append(out.Columns, spec.Status)
		baseline := map[string][]any{}
		for _, r := range base.Rows {
			k := key(r, rk, spec.Normalize)
			if _, dup := baseline[k]; dup {
				return nil, fmt.Errorf("duplicate key %s in %q", k, spec.With)
			}
			baseline[k] = r
		}
		value := func(tb *Table, r []any, c string) string {
			if n := tb.index(c); n >= 0 {
				return text(r[n])
			}
			return ""
		}
		emit := func(tb *Table, r []any, status string) {
			nr := make([]any, len(out.Columns))
			for i, c := range tb.Columns {
				nr[out.index(c)] = r[i]
			}
			nr[len(nr)-1] = status
			out.Rows = append(out.Rows, nr)
		}
		seen := map[string]bool{}
		for _, r := range t.Rows {
			k := key(r, lk, spec.Normalize)
			if seen[k] {
				return nil, fmt.Errorf("duplicate key %s in the current table", k)
			}
			seen[k] = true
			status := "added"
			if old, ok := baseline[k]; ok {
				status = "unchanged"
				for _, c := range compared {
					if value(t, r, c) != value(base, old, c) {
						status = "changed"
						break
					}
				}
			}
			emit(t, r, status)
		}
		for _, r := range base.Rows {
			if !seen[key(r, rk, spec.Normalize)] {
				emit(base, r, "removed")
			}
		}
		return out, nil
	}, nil
}

// ---- helpers ----------------------------------------------------------------

func (t *Table) clone() *Table {
	out := &Table{Columns: append([]string(nil), t.Columns...), Rows: make([][]any, len(t.Rows))}
	for i, r := range t.Rows {
		out.Rows[i] = append([]any(nil), r...)
	}
	return out
}

// addColumn returns the index of name, appending a blank column if needed.
func (t *Table) addColumn(name string) int {
	if n := t.index(name); n >= 0 {
		return n
	}
	t.Columns = append(t.Columns, name)
	for i := range t.Rows {
		t.Rows[i] = append(t.Rows[i], nil)
	}
	return len(t.Columns) - 1
}

func (t *Table) validate() error {
	seen := map[string]bool{}
	for _, c := range t.Columns {
		if strings.TrimSpace(c) == "" {
			return fmt.Errorf("blank column name")
		}
		if seen[c] {
			return fmt.Errorf("duplicate column %q", c)
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

func (t *Table) index(name string) int {
	for i, c := range t.Columns {
		if c == name {
			return i
		}
	}
	return -1
}

func (t *Table) col(name string) (int, error) {
	if n := t.index(name); n >= 0 {
		return n, nil
	}
	return 0, fmt.Errorf("unknown column %q", name)
}

func (t *Table) cols(names []string) ([]int, error) {
	out := make([]int, len(names))
	for i, name := range names {
		n, err := t.col(name)
		if err != nil {
			return nil, err
		}
		out[i] = n
	}
	return out, nil
}

func nameList(arg *yaml.Node) ([]string, error) {
	var names []string
	if err := strictDecode(arg, &names); err != nil || len(names) == 0 {
		return nil, fmt.Errorf("expects a list of names")
	}
	return names, nil
}

// orderedPairs reads a mapping as (key, value) pairs in file order.
func orderedPairs(arg *yaml.Node) ([][2]string, error) {
	if arg.Kind != yaml.MappingNode || len(arg.Content) == 0 {
		return nil, fmt.Errorf("expects a mapping")
	}
	var out [][2]string
	for i := 0; i < len(arg.Content); i += 2 {
		k, v := arg.Content[i], arg.Content[i+1]
		if v.Kind != yaml.ScalarNode || strings.TrimSpace(k.Value) == "" {
			return nil, fmt.Errorf("line %d: expects 'name: value'", k.Line)
		}
		out = append(out, [2]string{k.Value, v.Value})
	}
	return out, nil
}
