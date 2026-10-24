// Package sheet reads .xlsx and .xls (BIFF8) workbooks into plain string grids
// that match what the Node service saw through
//
//	XLSX.utils.sheet_to_json(ws, { header: 1, defval: '' })
//
// followed by String(value): row 0 / column 0 is the top-left cell of the
// sheet's declared range (NOT necessarily A1 -- SheetJS offsets by the range
// start, and the Node pipeline's hard-coded indexes inherit that), numbers
// are rendered the way JavaScript renders them, booleans become "true" and
// "false", errors and blanks become "".
//
// Both readers are streaming/purpose-built rather than general spreadsheet
// libraries: the service only ever needs raw cell values, and cell *types*
// are needed to reproduce JS number formatting exactly (a text cell "1.0"
// must stay "1.0", a numeric cell 1 must become "1").
package sheet

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// Workbook is an opened .xls / .xlsx file.
type Workbook interface {
	// SheetNames lists sheets in workbook order (hidden sheets included, as
	// SheetJS does).
	SheetNames() []string
	// Rows returns the named sheet's grid. A sheet without any cell data
	// returns nil (SheetJS: sheet_to_json -> []). Rows may be shorter than
	// the sheet's declared width; missing trailing cells are "".
	Rows(name string) ([][]string, error)
	Close() error
}

// ErrUnsupported is returned for files that are neither .xlsx nor BIFF8 .xls.
var ErrUnsupported = errors.New("unsupported file format: expected an Excel .xlsx or .xls (97-2003) workbook")

// Open sniffs the file's magic bytes (uploads are stored without an
// extension, and clients routinely mislabel .xls/.xlsx) and opens it.
func Open(path string) (Workbook, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	var magic [8]byte
	n, _ := io.ReadFull(f, magic[:])
	f.Close()
	switch {
	case n >= 4 && magic[0] == 'P' && magic[1] == 'K' && magic[2] == 3 && magic[3] == 4:
		return openXLSX(path)
	case n >= 8 && magic == [8]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}:
		return openXLS(path)
	}
	return nil, ErrUnsupported
}

// cellEntry is one non-empty cell at an absolute (0-based) position.
type cellEntry struct {
	row, col int
	v        string
}

// rng is an inclusive cell range, 0-based.
type rng struct{ r0, c0, r1, c1 int }

// buildGrid turns sparse entries into the sheet_to_json grid. dim is the
// sheet's declared range (nil if none); xlsMode unions the declared range
// with the data bounds (BIFF), otherwise a declared range clips the data
// (OOXML) exactly like SheetJS does.
func buildGrid(entries []cellEntry, dim *rng, xlsMode bool) [][]string {
	if len(entries) == 0 {
		if dim != nil {
			return [][]string{{""}} // non-empty sheet (SheetJS has a !ref), no data
		}
		return nil
	}
	data := rng{entries[0].row, entries[0].col, entries[0].row, entries[0].col}
	for _, e := range entries {
		if e.row < data.r0 {
			data.r0 = e.row
		}
		if e.row > data.r1 {
			data.r1 = e.row
		}
		if e.col < data.c0 {
			data.c0 = e.col
		}
		if e.col > data.c1 {
			data.c1 = e.col
		}
	}
	r := data
	switch {
	case dim == nil:
	case xlsMode:
		if dim.r0 < r.r0 {
			r.r0 = dim.r0
		}
		if dim.c0 < r.c0 {
			r.c0 = dim.c0
		}
	default:
		// Declared range wins: start where it starts, and never extend
		// past the real data (trailing blank rows/cols carry no
		// information and a sloppy A1:XFD1048576 dimension must not
		// allocate a million rows).
		r.r0, r.c0 = dim.r0, dim.c0
		if dim.r1 < r.r1 {
			r.r1 = dim.r1
		}
		if dim.c1 < r.c1 {
			r.c1 = dim.c1
		}
	}
	nRows, nCols := r.r1-r.r0+1, r.c1-r.c0+1
	if nRows <= 0 || nCols <= 0 {
		return [][]string{{""}}
	}
	if int64(nRows)*int64(nCols) > 200_000_000 {
		return nil // absurd; refuse rather than exhaust memory
	}
	grid := make([][]string, nRows)
	backing := make([]string, nRows*nCols)
	for i := range grid {
		grid[i] = backing[i*nCols : (i+1)*nCols : (i+1)*nCols]
	}
	for _, e := range entries {
		ri, ci := e.row-r.r0, e.col-r.c0
		if ri < 0 || ci < 0 || ri >= nRows || ci >= nCols {
			continue
		}
		grid[ri][ci] = e.v
	}
	return grid
}

func errf(format string, a ...any) error { return fmt.Errorf(format, a...) }
