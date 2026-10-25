package workflow

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

var headerColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// writeWorkbook writes the saved datasets as sheets, in declared order.
func writeWorkbook(ctx context.Context, out string, sheets []Sheet, tables map[string]*Table) error {
	f := excelize.NewFile()
	defer f.Close()
	for i, sh := range sheets {
		t, ok := tables[sh.Dataset]
		if !ok {
			return fmt.Errorf("output sheet %q: unknown dataset %q", sh.Name, sh.Dataset)
		}
		var err error
		if i == 0 {
			err = f.SetSheetName("Sheet1", sh.Name)
		} else {
			_, err = f.NewSheet(sh.Name)
		}
		if err != nil {
			return err
		}
		if err := writeTable(ctx, f, sh.Name, t); err != nil {
			return fmt.Errorf("output sheet %q: %w", sh.Name, err)
		}
	}
	return f.SaveAs(out)
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
			c, e := t.col(k)
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
