package bom

import "miservice/internal/jsx"

// Cell formatting model.
//
// ExcelJS applies a column-level alignment (center/middle) at the very end of
// writeSheet, which *replaces* every cell's own alignment -- so in the files
// the Node service produces every cell is center/middle and the left-align /
// right-align / wrapText settings in the Node source never survive. That is
// the behaviour to reproduce, so alignment is a constant here, not part of the
// per-cell format.

type side struct {
	style int // excelize border style: 1 thin, 2 medium, 5 thick
	color string
}
type borderSpec struct{ l, r, t, b side }
type fontSpec struct {
	set   bool // false = workbook default font
	bold  bool
	size  float64
	color string
}
type cellFmt struct {
	fill    string // RGB, "" = none
	font    fontSpec
	border  borderSpec
	numFmt3 bool // '#,##0'
}

// Palette.
const (
	yellow = "FFFF00"
	red    = "FF0000"
	black  = "000000"
	blue   = "0070C0"
	green  = "2E7D32"
	white  = "FFFFFF"

	debugHeaderFill = "2C3E50"
	thinGrayC       = "E0E0E0"
)

var (
	fontWhite       = fontSpec{true, true, 10, white}
	fontHeader      = fontSpec{true, true, 10, ""}
	fontDebugHeader = fontSpec{true, true, 10, white}
	fontDebugNormal = fontSpec{true, false, 9, "212121"}
	fontDebugBold   = fontSpec{true, true, 9, "212121"}
	fontDebugDim    = fontSpec{true, false, 9, "9E9E9E"}
	fontEligible    = fontSpec{true, true, 9, "2E7D32"}
	fontIneligible  = fontSpec{true, true, 9, "C62828"}
	fontOutScope    = fontSpec{true, false, 9, "757575"}
	fontP1Gold      = fontSpec{true, true, 9, "E65100"}
	fontStockPos    = fontSpec{true, true, 9, "1565C0"}
	fontReasonMain  = fontSpec{true, true, 9, "1565C0"}
	fontReasonSub   = fontSpec{true, false, 9, "33691E"}

	thickBlack = side{5, black}
	thinGray   = side{1, thinGrayC}
	debugLeft  = side{2, debugHeaderFill}
)

// val is one cell value: text, or a number (the debug stock column).
type val struct {
	s     string
	n     float64
	isNum bool
}

func str(s string) val { return val{s: s} }

func (v val) text() string {
	if v.isNum {
		return jsx.NumToString(v.n)
	}
	return v.s
}

// fmtTable interns distinct cell formats so each becomes one workbook style.
type fmtTable struct {
	list []cellFmt
	idx  map[cellFmt]uint16
}

func newFmtTable() *fmtTable {
	t := &fmtTable{idx: map[cellFmt]uint16{}}
	t.intern(cellFmt{}) // id 0 = plain cell
	return t
}

func (t *fmtTable) intern(f cellFmt) uint16 {
	if id, ok := t.idx[f]; ok {
		return id
	}
	id := uint16(len(t.list))
	t.list = append(t.list, f)
	t.idx[f] = id
	return id
}

// grid is a sheet in memory: values plus a format id per cell.
type grid struct {
	vals [][]val
	fmts [][]uint16
	tbl  *fmtTable
}

func newGrid() *grid { return &grid{tbl: newFmtTable()} }

// addRow appends a row of values and returns its index.
func (g *grid) addRow(vs []val) int {
	g.vals = append(g.vals, vs)
	g.fmts = append(g.fmts, make([]uint16, len(vs)))
	return len(g.vals) - 1
}

func (g *grid) mod(r, c int, fn func(*cellFmt)) {
	f := g.tbl.list[g.fmts[r][c]]
	fn(&f)
	g.fmts[r][c] = g.tbl.intern(f)
}

func (g *grid) fill(r, c int, rgb string)     { g.mod(r, c, func(f *cellFmt) { f.fill = rgb }) }
func (g *grid) font(r, c int, fs fontSpec)    { g.mod(r, c, func(f *cellFmt) { f.font = fs }) }
func (g *grid) border(r, c int, b borderSpec) { g.mod(r, c, func(f *cellFmt) { f.border = b }) }
func (g *grid) thousands(r, c int)            { g.mod(r, c, func(f *cellFmt) { f.numFmt3 = true }) }
