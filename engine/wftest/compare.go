package wftest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Difference describes the first mismatch between an output and its fixture.
type Difference struct {
	Kind           string   `json:"kind"`
	Sheet          string   `json:"sheet,omitempty"`
	Cell           string   `json:"cell,omitempty"`
	Line           int      `json:"line,omitempty"`
	Actual         string   `json:"actual"`
	Expected       string   `json:"expected"`
	ActualSheets   []string `json:"actualSheets,omitempty"`
	ExpectedSheets []string `json:"expectedSheets,omitempty"`
}

func (d *Difference) Error() string {
	switch d.Kind {
	case "cell":
		return fmt.Sprintf("sheet %q cell %s: got %q, expected %q", d.Sheet, d.Cell, d.Actual, d.Expected)
	case "sheets":
		return fmt.Sprintf("sheets: got %q, expected %q", d.ActualSheets, d.ExpectedSheets)
	default:
		return fmt.Sprintf("line %d: got %q, expected %q", d.Line, d.Actual, d.Expected)
	}
}

// compare reports the first difference between two outputs: for workbooks
// the sheet names and every cell's value (not styles), for CSV the text.
func compare(ctx context.Context, got, want string) (*Difference, error) {
	if !strings.EqualFold(filepath.Ext(got), ".xlsx") {
		g, err := os.ReadFile(got)
		if err != nil {
			return nil, err
		}
		w, err := os.ReadFile(want)
		if err != nil {
			return nil, err
		}
		gl, wl := lines(g), lines(w)
		for i := 0; i < len(gl) || i < len(wl); i++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if a, b := at(gl, i), at(wl, i); a != b {
				return &Difference{Kind: "line", Line: i + 1, Actual: a, Expected: b}, nil
			}
		}
		return nil, nil
	}
	w, err := excelize.OpenFile(want)
	if err != nil {
		return nil, err
	}
	defer w.Close()
	g, err := excelize.OpenFile(got)
	if err != nil {
		return nil, err
	}
	defer g.Close()
	if a, b := g.GetSheetList(), w.GetSheetList(); strings.Join(a, "\x00") != strings.Join(b, "\x00") {
		return &Difference{Kind: "sheets", ActualSheets: a, ExpectedSheets: b}, nil
	}
	for _, sheet := range w.GetSheetList() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		gr, err := g.GetRows(sheet)
		if err != nil {
			return nil, err
		}
		wr, err := w.GetRows(sheet)
		if err != nil {
			return nil, err
		}
		for r := 0; r < len(gr) || r < len(wr); r++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			var grow, wrow []string
			if r < len(gr) {
				grow = gr[r]
			}
			if r < len(wr) {
				wrow = wr[r]
			}
			for c := 0; c < len(grow) || c < len(wrow); c++ {
				if a, b := at(grow, c), at(wrow, c); a != b {
					cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
					return &Difference{Kind: "cell", Sheet: sheet, Cell: cell, Actual: a, Expected: b}, nil
				}
			}
		}
	}
	return nil, nil
}

func lines(b []byte) []string {
	return strings.Split(strings.TrimRight(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), "\n")
}

func at(list []string, i int) string {
	if i < len(list) {
		return list[i]
	}
	return ""
}
