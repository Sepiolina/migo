package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"miservice/internal/jsx"
	"miservice/internal/sheet"
)

// Table is a sheet of text cells with unique headers. Input formulas arrive
// as their cached values.
type Table struct {
	Columns []string
	Rows    [][]string
	Style   *Style
}

// Style is applied when the table is written to an output sheet.
type Style struct {
	Header        *HeaderStyle      `yaml:"header"`
	AutoWidth     bool              `yaml:"auto_width"`
	FreezeHeader  bool              `yaml:"freeze_header"`
	NumberFormats map[string]string `yaml:"number_formats"` // column -> Excel format; makes the column numeric
}

type HeaderStyle struct {
	Bold       bool   `yaml:"bold"`
	Background string `yaml:"background"` // #RRGGBB
	Color      string `yaml:"color"`      // #RRGGBB
}

// TableOps are the generic sheet steps. Each works on the current table.
func TableOps() Registry {
	return Registry{
		"from":    {Compile: compileFrom},
		"save":    {Compile: tableStep(compileSave)},
		"select":  {Compile: tableStep(compileSelect)},
		"rename":  {Compile: tableStep(compileRename)},
		"filter":  {Compile: tableStep(compileFilter)},
		"set":     {Compile: tableStep(compileSet)},
		"fill":    {Compile: tableStep(compileFill)},
		"dedupe":  {Compile: tableStep(compileDedupe)},
		"group":   {Compile: tableStep(compileGroup)},
		"sort":    {Compile: tableStep(compileSort)},
		"join":    {Compile: tableStep(compileJoin)},
		"append":  {Compile: tableStep(compileAppend)},
		"compare": {Compile: tableStep(compileCompare)},
		"style":   {Compile: tableStep(compileStyle)},
	}
}

// tableFn transforms the current table; t is a private copy it may modify.
type tableFn func(t *Table, s *State) (*Table, error)

// tableStep wraps a compiler of table functions so every step gets a copy
// (saved datasets never change) and a validated result.
func tableStep(compile func(arg *yaml.Node) (tableFn, error)) func(*yaml.Node) (Runner, error) {
	return func(arg *yaml.Node) (Runner, error) {
		if arg == nil {
			return nil, fmt.Errorf("needs an argument")
		}
		fn, err := compile(arg)
		if err != nil {
			return nil, err
		}
		return func(_ context.Context, s *State) error {
			if s.Cur == nil {
				return fmt.Errorf("no current table: start with a 'from' step")
			}
			t, err := fn(s.Cur.clone(), s)
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

// ---- from / save ------------------------------------------------------------

// from: input            first sheet of an uploaded file
// from: input[Sheet]     a named sheet
// from: dataset          a dataset saved earlier
func compileFrom(arg *yaml.Node) (Runner, error) {
	if arg == nil || arg.Kind != yaml.ScalarNode || strings.TrimSpace(arg.Value) == "" {
		return nil, fmt.Errorf("names an input, input[Sheet] or a saved dataset")
	}
	src, sheetName := arg.Value, ""
	if i := strings.IndexByte(src, '['); i >= 0 {
		if i == 0 || !strings.HasSuffix(src, "]") || len(src) < i+3 {
			return nil, fmt.Errorf("invalid source %q: use input[Sheet]", src)
		}
		src, sheetName = src[:i], src[i+1:len(src)-1]
	}
	return func(_ context.Context, s *State) error {
		if t, ok := s.Tables[src]; ok && sheetName == "" {
			s.Cur = t.clone()
			return nil
		}
		path, ok := s.Inputs[src]
		if !ok {
			return fmt.Errorf("unknown input or dataset %q", src)
		}
		t, err := readTable(path, sheetName)
		if err != nil {
			return fmt.Errorf("%s: %w", src, err)
		}
		s.Cur = t
		return nil
	}, nil
}

func readTable(path, name string) (*Table, error) {
	wb, err := sheet.Open(path)
	if err != nil {
		return nil, err
	}
	defer wb.Close()
	if name == "" {
		names := wb.SheetNames()
		if len(names) == 0 {
			return nil, fmt.Errorf("workbook has no sheets")
		}
		name = names[0]
	}
	grid, err := wb.Rows(name)
	if err != nil {
		return nil, fmt.Errorf("sheet %q: %w", name, err)
	}
	if len(grid) == 0 {
		return nil, fmt.Errorf("sheet %q is empty", name)
	}
	headers := append([]string(nil), grid[0]...)
	for len(headers) > 0 && headers[len(headers)-1] == "" {
		headers = headers[:len(headers)-1]
	}
	if len(headers) == 0 {
		return nil, fmt.Errorf("sheet %q: header row is empty", name)
	}
	t := &Table{Columns: headers}
	for i, row := range grid[1:] {
		for _, v := range row[min(len(row), len(headers)):] {
			if v != "" {
				return nil, fmt.Errorf("sheet %q row %d has data beyond the %d headers", name, i+2, len(headers))
			}
		}
		r := make([]string, len(headers))
		copy(r, row)
		t.Rows = append(t.Rows, r)
	}
	return t, t.validate()
}

func compileSave(arg *yaml.Node) (tableFn, error) {
	name := strings.TrimSpace(arg.Value)
	if arg.Kind != yaml.ScalarNode || name == "" {
		return nil, fmt.Errorf("names the dataset to save")
	}
	return func(t *Table, s *State) (*Table, error) {
		if _, clash := s.Inputs[name]; clash {
			return nil, fmt.Errorf("dataset %q would hide the input of the same name", name)
		}
		s.Tables[name] = t.clone()
		return t, nil
	}, nil
}

// ---- columns ----------------------------------------------------------------

// select: {New: Source, ...} keeps only these columns, in this order.
func compileSelect(arg *yaml.Node) (tableFn, error) {
	pairs, err := orderedPairs(arg)
	if err != nil {
		return nil, err
	}
	return func(t *Table, _ *State) (*Table, error) {
		src := make([]int, len(pairs))
		out := &Table{Style: t.Style}
		for i, p := range pairs {
			n, err := t.col(p[1])
			if err != nil {
				return nil, err
			}
			src[i] = n
			out.Columns = append(out.Columns, p[0])
		}
		for _, r := range t.Rows {
			nr := make([]string, len(src))
			for j, n := range src {
				nr[j] = r[n]
			}
			out.Rows = append(out.Rows, nr)
		}
		out.Style = t.projectStyle(src, out.Columns)
		return out, nil
	}, nil
}

// rename: {Old: New, ...} keeps every column.
func compileRename(arg *yaml.Node) (tableFn, error) {
	pairs, err := orderedPairs(arg)
	if err != nil {
		return nil, err
	}
	return func(t *Table, _ *State) (*Table, error) {
		src := make([]int, len(t.Columns))
		names := append([]string(nil), t.Columns...)
		for i := range src {
			src[i] = i
		}
		for _, p := range pairs {
			n, err := t.col(p[0])
			if err != nil {
				return nil, err
			}
			names[n] = p[1]
		}
		t.Style = t.projectStyle(src, names)
		t.Columns = names
		return t, nil
	}, nil
}

// fill: [Col, ...] forward-fills blank cells from the row above.
func compileFill(arg *yaml.Node) (tableFn, error) {
	names, err := columnList(arg)
	if err != nil {
		return nil, err
	}
	return func(t *Table, _ *State) (*Table, error) {
		ix, err := t.cols(names)
		if err != nil {
			return nil, err
		}
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
		return t, nil
	}, nil
}

// dedupe: [Col, ...] keeps the first row of each key.
func compileDedupe(arg *yaml.Node) (tableFn, error) {
	names, err := columnList(arg)
	if err != nil {
		return nil, err
	}
	return func(t *Table, _ *State) (*Table, error) {
		ix, err := t.cols(names)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		rows := t.Rows
		t.Rows = nil
		for _, r := range rows {
			if k := key(r, ix, false); !seen[k] {
				seen[k] = true
				t.Rows = append(t.Rows, r)
			}
		}
		return t, nil
	}, nil
}

// ---- expressions ------------------------------------------------------------

// filter: '<expression>' keeps the rows where it is true.
func compileFilter(arg *yaml.Node) (tableFn, error) {
	e, err := compileRowExpr(arg.Value, true)
	if err != nil {
		return nil, err
	}
	return func(t *Table, _ *State) (*Table, error) {
		rows := t.Rows
		t.Rows = nil
		for i, r := range rows {
			v, err := e.eval(t, r, i)
			if err != nil {
				return nil, err
			}
			if v.(bool) {
				t.Rows = append(t.Rows, r)
			}
		}
		return t, nil
	}, nil
}

// set: {Col: '<expression>', ...} computes columns in order; new ones are appended.
func compileSet(arg *yaml.Node) (tableFn, error) {
	pairs, err := orderedPairs(arg)
	if err != nil {
		return nil, err
	}
	exprs := make([]*rowExpr, len(pairs))
	for i, p := range pairs {
		if exprs[i], err = compileRowExpr(p[1], false); err != nil {
			return nil, fmt.Errorf("%s: %w", p[0], err)
		}
	}
	return func(t *Table, _ *State) (*Table, error) {
		for k, p := range pairs {
			dest := t.index(p[0])
			if dest < 0 {
				dest = len(t.Columns)
				t.Columns = append(t.Columns, p[0])
				for i := range t.Rows {
					t.Rows[i] = append(t.Rows[i], "")
				}
			}
			for i, r := range t.Rows {
				v, err := exprs[k].eval(t, r, i)
				if err != nil {
					return nil, err
				}
				if r[dest], err = toText(v); err != nil {
					return nil, fmt.Errorf("row %d column %q: %w", i+1, p[0], err)
				}
			}
		}
		return t, nil
	}, nil
}

// ---- group / sort -----------------------------------------------------------

var aggregates = []string{"sum", "count", "mean", "min", "max", "first", "last", "join"}

// group: {by: [Key], sum: [Qty], first: [Desc], ...}. Without by, the whole
// table becomes one row.
func compileGroup(arg *yaml.Node) (tableFn, error) {
	var spec map[string][]string
	if err := strictDecode(arg, &spec); err != nil {
		return nil, err
	}
	ops := map[string]string{}
	for op, columns := range spec {
		if op == "by" {
			continue
		}
		if !contains(aggregates, op) {
			return nil, fmt.Errorf("unknown aggregate %q (use by, %s)", op, strings.Join(aggregates, ", "))
		}
		for _, c := range columns {
			if _, dup := ops[c]; dup {
				return nil, fmt.Errorf("column %q aggregated twice", c)
			}
			ops[c] = op
		}
	}
	for _, c := range spec["by"] {
		if _, dup := ops[c]; dup {
			return nil, fmt.Errorf("column %q is both a key and aggregated", c)
		}
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("names at least one aggregated column")
	}
	return func(t *Table, _ *State) (*Table, error) { return groupTable(t, spec["by"], ops) }, nil
}

func groupTable(t *Table, by []string, ops map[string]string) (*Table, error) {
	keys, err := t.cols(by)
	if err != nil {
		return nil, err
	}
	for c := range ops {
		if _, err := t.col(c); err != nil {
			return nil, err
		}
	}
	src := append([]int(nil), keys...)
	out := &Table{}
	for _, n := range keys {
		out.Columns = append(out.Columns, t.Columns[n])
	}
	var agg []int // aggregated columns, in table order
	for i, c := range t.Columns {
		if _, ok := ops[c]; ok {
			agg = append(agg, i)
			out.Columns = append(out.Columns, c)
		}
	}
	out.Style = t.projectStyle(append(src, agg...), out.Columns)
	var groups [][]int
	lookup := map[string]int{}
	if len(keys) == 0 {
		groups = [][]int{nil}
	}
	for i, r := range t.Rows {
		k := key(r, keys, false)
		g, ok := lookup[k]
		if !ok && len(keys) > 0 {
			g = len(groups)
			lookup[k] = g
			groups = append(groups, nil)
		}
		groups[g] = append(groups[g], i)
	}
	for _, g := range groups {
		row := make([]string, 0, len(out.Columns))
		for _, n := range keys {
			row = append(row, t.Rows[g[0]][n])
		}
		for _, n := range agg {
			v, err := aggregate(t, g, n, ops[t.Columns[n]])
			if err != nil {
				return nil, err
			}
			row = append(row, v)
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

func aggregate(t *Table, rows []int, n int, op string) (string, error) {
	switch {
	case op == "count":
		return number(float64(len(rows)))
	case len(rows) == 0 && op == "sum":
		return "0", nil
	case len(rows) == 0:
		return "", nil
	case op == "first":
		return t.Rows[rows[0]][n], nil
	case op == "last":
		return t.Rows[rows[len(rows)-1]][n], nil
	case op == "join":
		seen := map[string]bool{}
		var values []string
		for _, r := range rows {
			if v := t.Rows[r][n]; strings.TrimSpace(v) != "" && !seen[v] {
				seen[v] = true
				values = append(values, v)
			}
		}
		return strings.Join(values, ", "), nil
	}
	v := 0.0
	for i, r := range rows {
		x, err := numeric(t.Rows[r][n], r, t.Columns[n])
		if err != nil {
			return "", err
		}
		switch {
		case op == "sum" || op == "mean":
			v += x
		case op == "min" && (i == 0 || x < v), op == "max" && (i == 0 || x > v):
			v = x
		}
	}
	if op == "mean" {
		v /= float64(len(rows))
	}
	return number(v)
}

// sort: {by: [Col, ...], desc: false, numeric: false}; stable.
func compileSort(arg *yaml.Node) (tableFn, error) {
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
		nums := make([][]float64, len(t.Rows))
		if spec.Numeric {
			for i, r := range t.Rows {
				for _, n := range ix {
					v, err := numeric(r[n], i, t.Columns[n])
					if err != nil {
						return nil, err
					}
					nums[i] = append(nums[i], v)
				}
			}
		}
		order := make([]int, len(t.Rows))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(a, b int) bool {
			ra, rb := order[a], order[b]
			for k, n := range ix {
				cmp := strings.Compare(t.Rows[ra][n], t.Rows[rb][n])
				if spec.Numeric {
					cmp = compareFloat(nums[ra][k], nums[rb][k])
				}
				if cmp != 0 {
					return (cmp < 0) != spec.Desc
				}
			}
			return false
		})
		rows := make([][]string, len(order))
		for i, n := range order {
			rows[i] = t.Rows[n]
		}
		t.Rows = rows
		return t, nil
	}, nil
}

// ---- other datasets ---------------------------------------------------------

// join: {with: dataset, on: [Key], right_on: [Key], how: left|inner, normalize: false, suffix: _right}
func compileJoin(arg *yaml.Node) (tableFn, error) {
	var spec struct {
		With      string   `yaml:"with"`
		On        []string `yaml:"on"`
		RightOn   []string `yaml:"right_on"`
		How       string   `yaml:"how"`
		Normalize bool     `yaml:"normalize"`
		Suffix    string   `yaml:"suffix"`
	}
	if err := strictDecode(arg, &spec); err != nil {
		return nil, err
	}
	if spec.With == "" || len(spec.On) == 0 {
		return nil, fmt.Errorf("needs with and on")
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
		out := &Table{Columns: append([]string(nil), t.Columns...), Style: t.Style}
		var extra []int
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
		lookup := map[string][][]string{}
		for _, r := range right.Rows {
			k := key(r, rk, spec.Normalize)
			lookup[k] = append(lookup[k], r)
		}
		for _, r := range t.Rows {
			matches := lookup[key(r, lk, spec.Normalize)]
			if len(matches) == 0 && spec.How != "inner" {
				out.Rows = append(out.Rows, append(append([]string(nil), r...), make([]string, len(extra))...))
			}
			for _, m := range matches {
				nr := append([]string(nil), r...)
				for _, n := range extra {
					nr = append(nr, m[n])
				}
				out.Rows = append(out.Rows, nr)
			}
		}
		return out, nil
	}, nil
}

// append: [dataset, ...] adds their rows; columns are matched by name.
func compileAppend(arg *yaml.Node) (tableFn, error) {
	names, err := columnList(arg)
	if err != nil {
		return nil, err
	}
	return func(t *Table, s *State) (*Table, error) {
		for _, name := range names {
			d, err := s.dataset(name)
			if err != nil {
				return nil, err
			}
			if len(d.Columns) != len(t.Columns) {
				return nil, fmt.Errorf("dataset %q has different columns", name)
			}
			ix, err := d.cols(t.Columns)
			if err != nil {
				return nil, fmt.Errorf("dataset %q: %w", name, err)
			}
			for _, r := range d.Rows {
				nr := make([]string, len(ix))
				for j, n := range ix {
					nr[j] = r[n]
				}
				t.Rows = append(t.Rows, nr)
			}
		}
		return t, nil
	}, nil
}

// compare: {with: baseline, by: [Key], columns: [..], normalize: false, status: status}
// adds a status column: added, removed, changed or unchanged.
func compileCompare(arg *yaml.Node) (tableFn, error) {
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
		out := &Table{Columns: append([]string(nil), t.Columns...), Style: t.Style}
		for _, c := range base.Columns {
			if out.index(c) < 0 {
				out.Columns = append(out.Columns, c)
			}
		}
		if out.index(spec.Status) >= 0 {
			return nil, fmt.Errorf("status column %q already exists", spec.Status)
		}
		out.Columns = append(out.Columns, spec.Status)
		baseline := map[string][]string{}
		for _, r := range base.Rows {
			k := key(r, rk, spec.Normalize)
			if _, dup := baseline[k]; dup {
				return nil, fmt.Errorf("duplicate key %s in %q", k, spec.With)
			}
			baseline[k] = r
		}
		cell := func(tb *Table, r []string, c string) string {
			if n := tb.index(c); n >= 0 {
				return r[n]
			}
			return ""
		}
		emit := func(tb *Table, r []string, status string) {
			nr := make([]string, len(out.Columns))
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
					if cell(t, r, c) != cell(base, old, c) {
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

// style: {header: {bold, background, color}, auto_width, freeze_header, number_formats: {Col: "0.00"}}
func compileStyle(arg *yaml.Node) (tableFn, error) {
	var st Style
	if err := strictDecode(arg, &st); err != nil {
		return nil, err
	}
	if h := st.Header; h != nil {
		for _, c := range []string{h.Color, h.Background} {
			if c != "" && !headerColor.MatchString(c) {
				return nil, fmt.Errorf("invalid color %q: use #RRGGBB", c)
			}
		}
	}
	return func(t *Table, _ *State) (*Table, error) {
		for c := range st.NumberFormats {
			if _, err := t.col(c); err != nil {
				return nil, err
			}
		}
		t.Style = st.clone()
		return t, nil
	}, nil
}

// ---- helpers ----------------------------------------------------------------

func (s *State) dataset(name string) (*Table, error) {
	t, ok := s.Tables[name]
	if !ok {
		return nil, fmt.Errorf("unknown dataset %q (save it first)", name)
	}
	return t, nil
}

func (t *Table) clone() *Table {
	out := &Table{Columns: append([]string(nil), t.Columns...), Style: t.Style.clone()}
	out.Rows = make([][]string, len(t.Rows))
	for i, r := range t.Rows {
		out.Rows[i] = append([]string(nil), r...)
	}
	return out
}

func (s *Style) clone() *Style {
	if s == nil {
		return nil
	}
	c := *s
	if s.Header != nil {
		h := *s.Header
		c.Header = &h
	}
	c.NumberFormats = map[string]string{}
	for k, v := range s.NumberFormats {
		c.NumberFormats[k] = v
	}
	return &c
}

// projectStyle carries number formats along when columns are selected,
// copied or renamed: names[i] is the new name of source column src[i].
func (t *Table) projectStyle(src []int, names []string) *Style {
	s := t.Style.clone()
	if s == nil {
		return nil
	}
	s.NumberFormats = map[string]string{}
	for i, n := range src {
		if f, ok := t.Style.NumberFormats[t.Columns[n]]; ok {
			s.NumberFormats[names[i]] = f
		}
	}
	return s
}

func (t *Table) validate() error {
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
		if contains(names[:i], name) {
			return nil, fmt.Errorf("column %q listed twice", name)
		}
		out[i] = n
	}
	return out, nil
}

func columnList(arg *yaml.Node) ([]string, error) {
	var names []string
	if err := strictDecode(arg, &names); err != nil || len(names) == 0 {
		return nil, fmt.Errorf("expects a list of names")
	}
	return names, nil
}

// orderedPairs reads a mapping as (key, value) pairs in file order.
func orderedPairs(arg *yaml.Node) ([][2]string, error) {
	if arg.Kind != yaml.MappingNode || len(arg.Content) == 0 {
		return nil, fmt.Errorf("expects a mapping of column names")
	}
	var out [][2]string
	for i := 0; i < len(arg.Content); i += 2 {
		k, v := arg.Content[i], arg.Content[i+1]
		if v.Kind != yaml.ScalarNode || strings.TrimSpace(k.Value) == "" {
			return nil, fmt.Errorf("line %d: expects 'column: value'", k.Line)
		}
		out = append(out, [2]string{k.Value, v.Value})
	}
	return out, nil
}

// numeric rejects blank cells, NaN and infinities.
func numeric(s string, row int, column string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("row %d column %q: invalid number %q", row+1, column, s)
	}
	return v, nil
}

func number(v float64) (string, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "", fmt.Errorf("result is not a finite number")
	}
	return strconv.FormatFloat(v, 'g', -1, 64), nil
}

func compareFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func key(row []string, ix []int, normalize bool) string {
	v := make([]string, len(ix))
	for i, n := range ix {
		v[i] = row[n]
		if normalize {
			v[i] = jsx.Norm(v[i])
		}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
