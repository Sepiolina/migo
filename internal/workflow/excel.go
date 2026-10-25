package workflow

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

type stagedWorkbook struct{ target, temp string }

var headerColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func exportWorkbooks(ctx context.Context, cfg *Config, tables map[string]*Table, publish bool) error {
	staged := make([]stagedWorkbook, 0, len(cfg.Output))
	defer func() {
		for _, s := range staged {
			if s.temp != "" {
				_ = os.Remove(s.temp)
			}
		}
	}()
	// Protect every input and template before staging, including aliases through
	// existing symlinks and hard links.
	protected := make([]string, 0, len(cfg.Input)+len(cfg.Output))
	for _, p := range cfg.Input {
		protected = append(protected, resolvePath(cfg.BaseDir, p))
	}
	for _, o := range cfg.Output {
		if o.Template != "" {
			protected = append(protected, resolvePath(cfg.BaseDir, o.Template))
		}
	}
	targets := make([]string, 0, len(cfg.Output))
	for i, o := range cfg.Output {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := resolvePath(cfg.BaseDir, o.File)
		if strings.ToLower(filepath.Ext(path)) != ".xlsx" {
			return fmt.Errorf("output %d: file must have .xlsx extension", i+1)
		}
		for _, p := range append(append([]string(nil), protected...), targets...) {
			if samePath(path, p) {
				return fmt.Errorf("output %d: %q collides with input, template, or another output %q", i+1, path, p)
			}
		}
		targets = append(targets, path)
	}
	for i, o := range cfg.Output {
		if err := ctx.Err(); err != nil {
			return err
		}
		temp, err := renderWorkbook(ctx, cfg, o, tables, targets[i])
		if err != nil {
			return fmt.Errorf("output %d (%s): %w", i+1, o.File, err)
		}
		staged = append(staged, stagedWorkbook{targets[i], temp})
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !publish {
		return nil
	}
	for i, s := range staged {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.Rename(s.temp, s.target); err != nil {
			return fmt.Errorf("publish %q (earlier outputs may already be published): %w", s.target, err)
		}
		staged[i].temp = ""
	}
	return nil
}

func samePath(a, b string) bool {
	aa, _ := filepath.Abs(a)
	bb, _ := filepath.Abs(b)
	if aa == bb {
		return true
	}
	ai, ae := os.Stat(aa)
	bi, be := os.Stat(bb)
	if ae == nil && be == nil && os.SameFile(ai, bi) {
		return true
	}
	canonical := func(p string) string {
		if v, e := filepath.EvalSymlinks(p); e == nil {
			return v
		}
		if d, e := filepath.EvalSymlinks(filepath.Dir(p)); e == nil {
			return filepath.Join(d, filepath.Base(p))
		}
		return p
	}
	return canonical(aa) == canonical(bb)
}

func renderWorkbook(ctx context.Context, cfg *Config, o Output, tables map[string]*Table, target string) (temp string, err error) {
	f := excelize.NewFile()
	if o.Template != "" {
		_ = f.Close()
		f, err = excelize.OpenFile(resolvePath(cfg.BaseDir, o.Template))
		if err != nil {
			return "", fmt.Errorf("template %q: %w", o.Template, err)
		}
	}
	defer func() {
		if e := f.Close(); err == nil && e != nil {
			err = e
		}
		if err != nil && temp != "" {
			_ = os.Remove(temp)
		}
	}()
	names := make([]string, 0, len(o.Sheets))
	for n := range o.Sheets {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "", fmt.Errorf("no sheets configured")
	}
	// Keep one temporary sheet so deleting the sole template/default sheet works.
	scratch := ""
	for i := 0; ; i++ {
		scratch = fmt.Sprintf("_workflow_%d", i)
		idx, _ := f.GetSheetIndex(scratch)
		reserved := false
		for name := range o.Sheets {
			reserved = reserved || strings.EqualFold(name, scratch)
		}
		if idx < 0 && !reserved {
			break
		}
	}
	if _, err = f.NewSheet(scratch); err != nil {
		return "", err
	}
	if o.Template == "" {
		if err = f.DeleteSheet("Sheet1"); err != nil {
			return "", err
		}
	}
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		t, ok := tables[o.Sheets[name]]
		if !ok {
			return "", fmt.Errorf("sheet %q references unknown dataset %q", name, o.Sheets[name])
		}
		if err = ValidateTable(t); err != nil {
			return "", fmt.Errorf("sheet %q: %w", name, err)
		}
		if idx, _ := f.GetSheetIndex(name); idx >= 0 {
			if err = f.DeleteSheet(name); err != nil {
				return "", err
			}
		}
		if _, err = f.NewSheet(name); err != nil {
			return "", err
		}
		if err = writeTable(ctx, f, name, t); err != nil {
			return "", fmt.Errorf("sheet %q: %w", name, err)
		}
	}
	if err = f.DeleteSheet(scratch); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".workflow-*.xlsx")
	if err != nil {
		return "", err
	}
	temp = tmp.Name()
	if err = tmp.Close(); err != nil {
		return temp, err
	}
	if err = f.SaveAs(temp); err != nil {
		return temp, err
	}
	return temp, nil
}

func writeTable(ctx context.Context, f *excelize.File, name string, t *Table) error {
	if len(t.Columns) > 16384 || len(t.Rows) > 1048575 {
		return fmt.Errorf("table exceeds XLSX row or column limits")
	}
	numeric := make(map[int]string)
	if t.Style != nil {
		keys := make([]string, 0, len(t.Style.NumberFormats))
		for k := range t.Style.NumberFormats {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			c, e := styleColumn(t, k)
			if e != nil {
				return e
			}
			if _, exists := numeric[c]; exists {
				return fmt.Errorf("duplicate number format for column %q", t.Columns[c])
			}
			numeric[c] = t.Style.NumberFormats[k]
		}
		if h := t.Style.Header; h != nil {
			for _, color := range []string{h.Color, h.Background} {
				if color != "" && !headerColor.MatchString(color) {
					return fmt.Errorf("invalid header color %q: expected #RRGGBB", color)
				}
			}
		}
	}
	for r := 0; r <= len(t.Rows); r++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		row := t.Columns
		if r > 0 {
			row = t.Rows[r-1]
		}
		for c, v := range row {
			cell, err := excelize.CoordinatesToCellName(c+1, r+1)
			if err != nil {
				return err
			}
			// Excelize silently truncates overlong strings. Reject them instead of
			// losing data produced by an aggregation or text transformation.
			if utf8.RuneCountInString(v) > excelize.TotalCellChars {
				return fmt.Errorf("cell %s exceeds XLSX limit of %d characters", cell, excelize.TotalCellChars)
			}
			// Number formats explicitly opt columns into numeric cells. Everything
			// else stays text, preserving identifiers and literal leading '='.
			if _, convert := numeric[c]; convert && r > 0 && strings.TrimSpace(v) != "" {
				n, e := strconv.ParseFloat(strings.TrimSpace(v), 64)
				if e != nil || math.IsInf(n, 0) || math.IsNaN(n) {
					return fmt.Errorf("row %d column %q: invalid finite number %q", r+1, t.Columns[c], v)
				}
				err = f.SetCellFloat(name, cell, n, -1, 64)
			} else {
				err = f.SetCellStr(name, cell, v)
			}
			if err != nil {
				return err
			}
		}
	}
	end, _ := excelize.CoordinatesToCellName(len(t.Columns), len(t.Rows)+1)
	if err := f.SetSheetDimension(name, "A1:"+end); err != nil {
		return err
	}
	if t.Style == nil {
		return nil
	}
	s := t.Style
	last, err := excelize.CoordinatesToCellName(len(t.Columns), 1)
	if err != nil {
		return err
	}
	if s.Header != nil {
		h := s.Header
		st := &excelize.Style{Font: &excelize.Font{Bold: h.Bold, Color: strings.TrimPrefix(h.Color, "#")}}
		if h.Background != "" {
			st.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{strings.TrimPrefix(h.Background, "#")}}
		}
		id, e := f.NewStyle(st)
		if e != nil {
			return e
		}
		if e = f.SetCellStyle(name, "A1", last, id); e != nil {
			return e
		}
	}
	if s.FreezeHeader {
		if err = f.SetPanes(name, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
			return err
		}
	}
	if s.AutoWidth {
		for c, h := range t.Columns {
			width := utf8.RuneCountInString(h)
			for _, row := range t.Rows {
				if n := utf8.RuneCountInString(row[c]); n > width {
					width = n
				}
			}
			w := float64(width + 2)
			if w < 8 {
				w = 8
			}
			if w > 60 {
				w = 60
			}
			col, _ := excelize.ColumnNumberToName(c + 1)
			if err = f.SetColWidth(name, col, col, w); err != nil {
				return err
			}
		}
	}
	for c := range t.Columns {
		format, configured := numeric[c]
		if !configured {
			continue
		}
		if len(t.Rows) == 0 {
			continue
		}
		id, e := f.NewStyle(&excelize.Style{CustomNumFmt: &format})
		if e != nil {
			return e
		}
		first, _ := excelize.CoordinatesToCellName(c+1, 2)
		end, _ := excelize.CoordinatesToCellName(c+1, len(t.Rows)+1)
		if e = f.SetCellStyle(name, first, end, id); e != nil {
			return e
		}
	}
	return nil
}

func styleColumn(t *Table, key string) (int, error) {
	if strings.HasPrefix(key, "@") {
		n, e := excelize.ColumnNameToNumber(strings.TrimPrefix(key, "@"))
		if e == nil && n > 0 && n <= len(t.Columns) {
			return n - 1, nil
		}
		return 0, fmt.Errorf("unknown column %q", key)
	}
	for i, h := range t.Columns {
		if h == key {
			return i, nil
		}
	}
	return 0, fmt.Errorf("unknown column %q", key)
}
