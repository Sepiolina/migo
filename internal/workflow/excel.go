package workflow

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
	"gopkg.in/yaml.v3"
)

// SheetSpec is one output sheet. The short form `Name: dataset` writes every
// column as is; the long form chooses and styles the columns:
//
//   - name: BOM
//     from: bom                      # a saved dataset
//     freeze: true                   # keep the header row in view
//     header: {bold: true, height: 28}
//     style: {align: center}         # every cell
//     columns:
//   - {header: Level, column: Lvl, width: 6}        # column: a dataset column (default: the header)
//   - {header: Total, value: 'num(Qty) * num(Price)', id: total, number_format: "#,##0.00"}
//     styles:                        # in order; later rules override earlier ones
//   - when: 'Status == "late"'   # row condition over the dataset's columns
//     columns: [total]           # ids (default: the header); none = the whole row
//     fill: "#FF0000"
//     font: {color: "#FFFFFF", bold: true}
//     border: {bottom: thick}
type SheetSpec struct {
	Name    string
	From    string
	Freeze  bool
	Header  *CellStyle
	Height  float64
	Base    *CellStyle
	Columns []*OutColumn // nil: every column of the dataset
	Styles  []*StyleRule
}

// OutColumn is one output column.
type OutColumn struct {
	Header string
	ID     string
	Width  float64
	Format string     // number format of numeric cells
	Style  *CellStyle // every data cell of the column
	Head   *CellStyle // the header cell
	value  *program   // a column name, or an expression over the row
}

// StyleRule styles cells of the rows where When holds.
type StyleRule struct {
	When    *program
	Columns []string
	Style   CellStyle
}

// CellStyle is a set of formatting properties; empty ones are not set.
type CellStyle struct {
	Fill   string `yaml:"fill"`
	Align  string `yaml:"align"`
	Format string `yaml:"number_format"`
	Font   *struct {
		Bold  *bool   `yaml:"bold"`
		Size  float64 `yaml:"size"`
		Color string  `yaml:"color"`
	} `yaml:"font"`
	Bold   *bool `yaml:"bold"` // shorthand for font.bold
	Border *struct {
		Left   string `yaml:"left"`
		Right  string `yaml:"right"`
		Top    string `yaml:"top"`
		Bottom string `yaml:"bottom"`
		Color  string `yaml:"color"`
	} `yaml:"border"`
}

var (
	colorRe      = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	borderStyles = map[string]int{"": 0, "thin": 1, "medium": 2, "dashed": 3, "dotted": 4, "thick": 5, "double": 6}
	sheetName    = regexp.MustCompile(`^[^\[\]:*?/\\]{1,31}$`)
)

func (c *CellStyle) check() error {
	if c == nil {
		return nil
	}
	colors := []string{c.Fill}
	if c.Font != nil {
		colors = append(colors, c.Font.Color)
	}
	if c.Border != nil {
		colors = append(colors, c.Border.Color)
		for _, b := range []string{c.Border.Left, c.Border.Right, c.Border.Top, c.Border.Bottom} {
			if _, ok := borderStyles[b]; !ok {
				return fmt.Errorf("border %q: use thin, medium, dashed, dotted, thick or double", b)
			}
		}
	}
	for _, col := range colors {
		if col != "" && !colorRe.MatchString(col) {
			return fmt.Errorf("color %q: use #RRGGBB", col)
		}
	}
	switch c.Align {
	case "", "left", "center", "right":
	default:
		return fmt.Errorf("align %q: use left, center or right", c.Align)
	}
	return nil
}

// merge lays o over c (o's set properties win).
func (c CellStyle) merge(o *CellStyle) CellStyle {
	if o == nil {
		return c
	}
	if o.Fill != "" {
		c.Fill = o.Fill
	}
	if o.Align != "" {
		c.Align = o.Align
	}
	if o.Format != "" {
		c.Format = o.Format
	}
	if o.Bold != nil {
		o2 := *o
		if o2.Font == nil {
			o2.Font = &struct {
				Bold  *bool   `yaml:"bold"`
				Size  float64 `yaml:"size"`
				Color string  `yaml:"color"`
			}{}
		} else {
			f := *o2.Font
			o2.Font = &f
		}
		o2.Font.Bold = o.Bold
		o = &o2
	}
	if o.Font != nil {
		f := *o.Font
		if c.Font != nil {
			base := *c.Font
			if f.Bold == nil {
				f.Bold = base.Bold
			}
			if f.Size == 0 {
				f.Size = base.Size
			}
			if f.Color == "" {
				f.Color = base.Color
			}
		}
		c.Font = &f
	}
	if o.Border != nil {
		b := *o.Border
		if c.Border != nil {
			base := *c.Border
			for _, p := range []struct{ dst, src *string }{{&b.Left, &base.Left}, {&b.Right, &base.Right}, {&b.Top, &base.Top}, {&b.Bottom, &base.Bottom}, {&b.Color, &base.Color}} {
				if *p.dst == "" {
					*p.dst = *p.src
				}
			}
		}
		c.Border = &b
	}
	return c
}

// key identifies a style by value (for the per-sheet style cache).
func (c CellStyle) key() string {
	k := fmt.Sprintf("%q|%q|%q", c.Fill, c.Align, c.Format)
	if c.Font != nil {
		bold := "-"
		if c.Font.Bold != nil {
			bold = fmt.Sprint(*c.Font.Bold)
		}
		k += fmt.Sprintf("|font:%s,%v,%q", bold, c.Font.Size, c.Font.Color)
	}
	if c.Border != nil {
		k += fmt.Sprintf("|border:%+v", *c.Border)
	}
	return k
}

func (c CellStyle) excel() *excelize.Style {
	st := &excelize.Style{}
	if c.Fill != "" {
		st.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{strings.TrimPrefix(c.Fill, "#")}}
	}
	if c.Align != "" {
		st.Alignment = &excelize.Alignment{Horizontal: c.Align, Vertical: "center"}
	}
	if c.Font != nil {
		st.Font = &excelize.Font{Size: c.Font.Size, Color: strings.TrimPrefix(c.Font.Color, "#")}
		if c.Font.Bold != nil {
			st.Font.Bold = *c.Font.Bold
		}
	}
	if c.Border != nil {
		color := strings.TrimPrefix(c.Border.Color, "#")
		if color == "" {
			color = "000000"
		}
		for _, b := range []struct{ side, kind string }{{"left", c.Border.Left}, {"right", c.Border.Right}, {"top", c.Border.Top}, {"bottom", c.Border.Bottom}} {
			if s := borderStyles[b.kind]; s != 0 {
				st.Border = append(st.Border, excelize.Border{Type: b.side, Style: s, Color: color})
			}
		}
	}
	if c.Format != "" {
		f := c.Format
		st.CustomNumFmt = &f
	}
	return st
}

func parseSheets(n *yaml.Node) ([]*SheetSpec, error) {
	var out []*SheetSpec
	switch n.Kind {
	case 0:
		return nil, nil
	case yaml.MappingNode: // Name: dataset
		pairs, err := orderedPairs(n)
		if err != nil {
			return nil, fmt.Errorf("output.sheets: %w", err)
		}
		for _, p := range pairs {
			out = append(out, &SheetSpec{Name: p[0], From: p[1]})
		}
	case yaml.SequenceNode:
		for _, item := range n.Content {
			sh, err := parseSheet(item, out)
			if err != nil {
				return nil, err
			}
			out = append(out, sh)
		}
	default:
		return nil, fmt.Errorf("output.sheets: a mapping of sheet: dataset, or a list of sheets")
	}
	seen := map[string]bool{}
	for _, sh := range out {
		if !sheetName.MatchString(sh.Name) || strings.TrimSpace(sh.Name) != sh.Name {
			return nil, fmt.Errorf("output sheet %q: invalid Excel sheet name", sh.Name)
		}
		if seen[strings.ToLower(sh.Name)] {
			return nil, fmt.Errorf("output sheet %q: duplicate (sheet names ignore case)", sh.Name)
		}
		seen[strings.ToLower(sh.Name)] = true
	}
	return out, nil
}

// parseSheet reads one sheet; like: <earlier sheet> starts from that
// sheet's columns and styles (its own are appended).
func parseSheet(n *yaml.Node, earlier []*SheetSpec) (*SheetSpec, error) {
	var spec struct {
		Name    string     `yaml:"name"`
		Like    string     `yaml:"like"`
		From    string     `yaml:"from"`
		Freeze  bool       `yaml:"freeze"`
		Header  *CellStyle `yaml:"header"`
		Height  float64    `yaml:"header_height"`
		Style   *CellStyle `yaml:"style"`
		Columns []struct {
			Header string     `yaml:"header"`
			ID     string     `yaml:"id"`
			Column string     `yaml:"column"`
			Value  string     `yaml:"value"`
			Width  float64    `yaml:"width"`
			Format string     `yaml:"number_format"`
			Style  *CellStyle `yaml:"style"`
			Head   *CellStyle `yaml:"header_style"`
		} `yaml:"columns"`
		Styles []struct {
			When      string   `yaml:"when"`
			Columns   []string `yaml:"columns"`
			CellStyle `yaml:",inline"`
		} `yaml:"styles"`
	}
	if err := strictDecode(n, &spec); err != nil {
		return nil, err
	}
	sh := &SheetSpec{Name: spec.Name, From: spec.From, Freeze: spec.Freeze, Header: spec.Header, Height: spec.Height, Base: spec.Style}
	where := fmt.Sprintf("output sheet %q", spec.Name)
	ids := map[string]bool{}
	if spec.Like != "" {
		var base *SheetSpec
		for _, e := range earlier {
			if e.Name == spec.Like {
				base = e
			}
		}
		if base == nil || base.Columns == nil {
			return nil, fmt.Errorf("%s: like %q must name an earlier sheet with columns", where, spec.Like)
		}
		copied := *base
		copied.Name = spec.Name
		copied.Columns = append([]*OutColumn(nil), base.Columns...)
		copied.Styles = append([]*StyleRule(nil), base.Styles...)
		for _, c := range base.Columns {
			ids[c.ID] = true
		}
		if spec.From != "" {
			copied.From = spec.From
		}
		if spec.Header != nil {
			copied.Header = spec.Header
		}
		if spec.Style != nil {
			copied.Base = spec.Style
		}
		if spec.Height > 0 {
			copied.Height = spec.Height
		}
		copied.Freeze = copied.Freeze || spec.Freeze
		sh = &copied
	}
	if sh.From == "" {
		return nil, fmt.Errorf("%s: needs from", where)
	}
	for _, c := range []*CellStyle{spec.Header, spec.Style} {
		if err := c.check(); err != nil {
			return nil, fmt.Errorf("%s: %w", where, err)
		}
	}
	for i, c := range spec.Columns {
		oc := &OutColumn{Header: c.Header, ID: c.ID, Width: c.Width, Format: c.Format, Style: c.Style, Head: c.Head}
		if oc.ID == "" {
			oc.ID = c.Header
		}
		switch {
		case c.Value != "" && c.Column != "":
			return nil, fmt.Errorf("%s column %d: use column or value, not both", where, i+1)
		case c.Value == "" && c.Column != "":
			c.Value = "col(" + quote(c.Column) + ")"
		case c.Value == "":
			c.Value = "col(" + quote(c.Header) + ")"
		}
		var err error
		if oc.value, err = compileExpr(c.Value, true, rowVars...); err != nil {
			return nil, fmt.Errorf("%s column %d: %w", where, i+1, err)
		}
		for _, st := range []*CellStyle{c.Style, c.Head} {
			if err := st.check(); err != nil {
				return nil, fmt.Errorf("%s column %d: %w", where, i+1, err)
			}
		}
		ids[oc.ID] = true
		sh.Columns = append(sh.Columns, oc)
	}
	for i, r := range spec.Styles {
		rule := &StyleRule{Columns: r.Columns, Style: r.CellStyle}
		if err := rule.Style.check(); err != nil {
			return nil, fmt.Errorf("%s style %d: %w", where, i+1, err)
		}
		if r.When != "" {
			var err error
			if rule.When, err = compileExpr(r.When, true, rowVars...); err != nil {
				return nil, fmt.Errorf("%s style %d: %w", where, i+1, err)
			}
		}
		if sh.Columns != nil {
			for _, id := range r.Columns {
				if !ids[id] {
					return nil, fmt.Errorf("%s style %d: unknown column id %q", where, i+1, id)
				}
			}
		}
		sh.Styles = append(sh.Styles, rule)
	}
	return sh, nil
}

func quote(s string) string { return "`" + strings.ReplaceAll(s, "`", "") + "`" }

// writeWorkbook writes the output sheets, in declared order.
func writeWorkbook(ctx context.Context, out string, sheets []*SheetSpec, s *State) error {
	f := excelize.NewFile()
	defer f.Close()
	for i, sh := range sheets {
		t, err := s.dataset(sh.From)
		if err != nil {
			return fmt.Errorf("output sheet %q: %w", sh.Name, err)
		}
		if i == 0 {
			err = f.SetSheetName("Sheet1", sh.Name)
		} else {
			_, err = f.NewSheet(sh.Name)
		}
		if err != nil {
			return err
		}
		if err := writeSheet(ctx, f, sh, t, s); err != nil {
			return fmt.Errorf("output sheet %q: %w", sh.Name, err)
		}
	}
	return f.SaveAs(out)
}

func writeSheet(ctx context.Context, f *excelize.File, sh *SheetSpec, t *Table, s *State) error {
	cols := sh.Columns
	if cols == nil {
		for _, c := range t.Columns {
			p, _ := compileExpr("col("+quote(c)+")", true, rowVars...)
			cols = append(cols, &OutColumn{Header: c, ID: c, value: p})
		}
	}
	if len(cols) > 16384 || len(t.Rows) > 1048575 {
		return fmt.Errorf("table exceeds XLSX row or column limits")
	}
	for _, c := range cols {
		if err := c.value.check(t); err != nil {
			return err
		}
	}
	for _, r := range sh.Styles {
		if r.When != nil {
			if err := r.When.check(t); err != nil {
				return err
			}
		}
	}
	styles := map[string]int{}
	styleID := func(c CellStyle) (int, error) {
		k := c.key()
		if id, ok := styles[k]; ok {
			return id, nil
		}
		id, err := f.NewStyle(c.excel())
		styles[k] = id
		return id, err
	}
	put := func(r, c int, v any, st CellStyle) error {
		axis, _ := excelize.CoordinatesToCellName(c+1, r+1)
		var err error
		switch x := cell(v).(type) {
		case nil:
		case float64:
			if math.IsNaN(x) || math.IsInf(x, 0) {
				return fmt.Errorf("cell %s: not a finite number", axis)
			}
			err = f.SetCellFloat(sh.Name, axis, x, -1, 64)
		case bool:
			err = f.SetCellBool(sh.Name, axis, x)
		case string:
			if utf8.RuneCountInString(x) > excelize.TotalCellChars {
				return fmt.Errorf("cell %s exceeds XLSX limit of %d characters", axis, excelize.TotalCellChars)
			}
			err = f.SetCellStr(sh.Name, axis, x)
		}
		if err != nil {
			return err
		}
		id, err := styleID(st)
		if err != nil {
			return err
		}
		return f.SetCellStyle(sh.Name, axis, axis, id)
	}
	base := CellStyle{}.merge(sh.Base)
	for c, col := range cols {
		if err := put(0, c, col.Header, base.merge(&CellStyle{Bold: boolPtr(true)}).merge(sh.Header).merge(col.Head)); err != nil {
			return err
		}
		name, _ := excelize.ColumnNumberToName(c + 1)
		if col.Width > 0 {
			if err := f.SetColWidth(sh.Name, name, name, col.Width); err != nil {
				return err
			}
		}
	}
	for i := range t.Rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		env := rowEnv(t, i, s.vars())
		row := make([]CellStyle, len(cols))
		for c, col := range cols {
			row[c] = base.merge(col.Style)
			if col.Format != "" {
				row[c].Format = col.Format
			}
		}
		for _, r := range sh.Styles {
			if r.When != nil {
				ok, err := r.When.test(env)
				if err != nil {
					return fmt.Errorf("row %d: %w", i+1, err)
				}
				if !ok {
					continue
				}
			}
			for c, col := range cols {
				if len(r.Columns) == 0 || contains(r.Columns, col.ID) {
					row[c] = row[c].merge(&r.Style)
				}
			}
		}
		for c, col := range cols {
			v, err := col.value.run(env)
			if err != nil {
				return fmt.Errorf("row %d column %q: %w", i+1, col.Header, err)
			}
			if err := put(i+1, c, v, row[c]); err != nil {
				return err
			}
		}
	}
	if sh.Height > 0 {
		if err := f.SetRowHeight(sh.Name, 1, sh.Height); err != nil {
			return err
		}
	}
	if sh.Freeze {
		return f.SetPanes(sh.Name, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	}
	return nil
}

func boolPtr(b bool) *bool { return &b }
