package bom

import (
	"github.com/xuri/excelize/v2"
)

// WriteOutput writes the two-sheet workbook ("BOM" + "debug") to outPath.
func WriteOutput(rows []*Row, outPath string, policy *VendorPolicy) error {
	if policy == nil {
		policy = DefaultVendorPolicy
	}
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetSheetName("Sheet1", StandardSheetName); err != nil {
		return err
	}
	if _, err := f.NewSheet(DebugSheetName); err != nil {
		return err
	}
	if err := writeSheet(f, StandardSheetName, rows, false, policy); err != nil {
		return err
	}
	if err := writeSheet(f, DebugSheetName, rows, true, policy); err != nil {
		return err
	}
	return f.SaveAs(outPath)
}

// writeSheet builds the sheet in memory (sheetbuilder.go) and copies it into
// the excelize workbook with its styles, widths and frozen header.
func writeSheet(f *excelize.File, name string, rows []*Row, debugMode bool, policy *VendorPolicy) error {
	g := buildSheet(rows, debugMode, policy)
	styleIDs, err := registerStyles(f, g.tbl)
	if err != nil {
		return err
	}
	if err := setColumns(f, name, debugMode); err != nil {
		return err
	}
	for r, rowVals := range g.vals {
		for c, v := range rowVals {
			axis, _ := excelize.CoordinatesToCellName(c+1, r+1)
			var err error
			if v.isNum {
				err = f.SetCellFloat(name, axis, v.n, -1, 64)
			} else {
				err = f.SetCellStr(name, axis, v.s)
			}
			if err != nil {
				return err
			}
			if err = f.SetCellStyle(name, axis, axis, styleIDs[g.fmts[r][c]]); err != nil {
				return err
			}
		}
	}
	if err := f.SetRowHeight(name, 1, 28); err != nil {
		return err
	}
	return f.SetPanes(name, &excelize.Panes{
		Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft",
		Selection: []excelize.Selection{{SQRef: "A2", ActiveCell: "A2", Pane: "bottomLeft"}},
	})
}

// registerStyles turns every interned cell format into a workbook style.
func registerStyles(f *excelize.File, tbl *fmtTable) ([]int, error) {
	ids := make([]int, len(tbl.list))
	for i, fm := range tbl.list {
		id, err := toExcelizeStyle(f, fm)
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}

// centered is the alignment of every cell (see format.go for why).
func centered() *excelize.Alignment {
	return &excelize.Alignment{Horizontal: "center", Vertical: "center"}
}

func toExcelizeStyle(f *excelize.File, fm cellFmt) (int, error) {
	st := &excelize.Style{Alignment: centered()}
	if fm.fill != "" {
		st.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{fm.fill}}
	}
	if fm.font.set {
		st.Font = &excelize.Font{Family: "Calibri", Size: fm.font.size, Bold: fm.font.bold, Color: fm.font.color}
		if fm.font.color == "" {
			theme := 1 // automatic text colour, as in the default font
			st.Font.ColorTheme = &theme
		}
	}
	add := func(kind string, s side) {
		if s.style != 0 {
			st.Border = append(st.Border, excelize.Border{Type: kind, Style: s.style, Color: s.color})
		}
	}
	add("left", fm.border.l)
	add("right", fm.border.r)
	add("top", fm.border.t)
	add("bottom", fm.border.b)
	if fm.numFmt3 {
		st.NumFmt = 3
	}
	return f.NewStyle(st)
}

// setColumns sets every column's width and the (centred) column style.
func setColumns(f *excelize.File, sheetName string, debugMode bool) error {
	widths := append([]float64(nil), colWidths...)
	if debugMode {
		widths = append(widths, debugColWidths...)
	}
	alignOnly, err := f.NewStyle(&excelize.Style{Alignment: centered()})
	if err != nil {
		return err
	}
	for i, w := range widths {
		col, _ := excelize.ColumnNumberToName(i + 1)
		if err := f.SetColWidth(sheetName, col, col, w); err != nil {
			return err
		}
		if err := f.SetColStyle(sheetName, col, alignOnly); err != nil {
			return err
		}
	}
	return nil
}
