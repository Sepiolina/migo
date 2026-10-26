package workflow

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
	"gopkg.in/yaml.v3"

	"miservice/internal/jsx"
	"miservice/internal/sheet"
)

// fromSpec is the from step argument.
//
//	from: parts                 first sheet of an input, row 1 as header (or a saved dataset)
//	from: parts[Parts]          a named sheet
//	from:
//	  input: bom
//	  sheet: 1                  1-based index or name (default: the first)
//	  header:                   find the header row by accepted titles
//	    columns: {Level: [level, lvl], Qty: [qty, quantity]}
//	    search: 15              ... within the first 15 rows
//	    min: 4                  ... where at least 4 columns match
//	    fallback: {Level: A, Qty: Z}   positions of unmatched / undetected columns
//	  columns: {PN: A, Value: M}       or: fixed positions, no header
//	  start: 168                first data row (default: the row after the header, else 2)
//	  trim: true                trim cells (JavaScript whitespace)
//	  skip_blank: true          drop rows whose selected cells are all blank
//	  row_number: Row           add the 1-based sheet row as a column
//	  empty: "No data."         fail with this message when the sheet has no cells
type fromSpec struct {
	Input  string    `yaml:"input"`
	Sheet  yaml.Node `yaml:"sheet"`
	Header *struct {
		Columns  yaml.Node         `yaml:"columns"`
		Search   int               `yaml:"search"`
		Min      int               `yaml:"min"`
		Fallback map[string]string `yaml:"fallback"`
	} `yaml:"header"`
	Columns   yaml.Node `yaml:"columns"`
	Start     int       `yaml:"start"`
	Trim      bool      `yaml:"trim"`
	SkipBlank bool      `yaml:"skip_blank"`
	RowNumber string    `yaml:"row_number"`
	Empty     string    `yaml:"empty"`
}

type headerCol struct {
	name     string
	titles   map[string]bool // normalised accepted titles
	fallback int             // 0-based, -1 none
}

func compileFrom(arg *yaml.Node, sc *scope) (Runner, error) {
	if arg == nil {
		return nil, fmt.Errorf("names an input, input[Sheet] or a saved dataset")
	}
	if arg.Kind == yaml.ScalarNode {
		src, sheetName := arg.Value, ""
		if i := strings.IndexByte(src, '['); i >= 0 {
			if i == 0 || !strings.HasSuffix(src, "]") || len(src) < i+3 {
				return nil, fmt.Errorf("invalid source %q: use input[Sheet]", src)
			}
			src, sheetName = src[:i], src[i+1:len(src)-1]
		}
		if sc.saved[src] && sheetName == "" {
			return func(_ context.Context, s *State) error {
				t, err := s.dataset(src)
				if err == nil {
					s.Cur = t.clone()
				}
				return err
			}, nil
		}
		if !sc.inputs[src] {
			return nil, fmt.Errorf("unknown input or dataset %q", src)
		}
		return readStep(readOpts{input: src, sheet: sheetName}), nil
	}
	var spec fromSpec
	if err := strictDecode(arg, &spec); err != nil {
		return nil, err
	}
	if !sc.inputs[spec.Input] {
		return nil, fmt.Errorf("unknown input %q", spec.Input)
	}
	sheetName, sheetIndex := "", 0
	if spec.Sheet.Kind != 0 {
		if n, err := strconv.Atoi(spec.Sheet.Value); err == nil && spec.Sheet.Tag == "!!int" {
			if n < 1 {
				return nil, fmt.Errorf("sheet index starts at 1")
			}
			sheetIndex = n - 1
		} else {
			sheetName = spec.Sheet.Value
		}
	}
	var cols []headerCol
	if spec.Header != nil {
		pairs := spec.Header.Columns
		if pairs.Kind != yaml.MappingNode || len(pairs.Content) == 0 {
			return nil, fmt.Errorf("header.columns maps column names to accepted titles")
		}
		for i := 0; i < len(pairs.Content); i += 2 {
			var titles []string
			if err := strictDecode(pairs.Content[i+1], &titles); err != nil || len(titles) == 0 {
				return nil, fmt.Errorf("header.columns.%s: expects a list of titles", pairs.Content[i].Value)
			}
			hc := headerCol{name: pairs.Content[i].Value, titles: map[string]bool{}, fallback: -1}
			for _, t := range titles {
				hc.titles[jsx.Norm(t)] = true
			}
			if pos, ok := spec.Header.Fallback[hc.name]; ok {
				n, err := columnNumber(pos)
				if err != nil {
					return nil, fmt.Errorf("header.fallback.%s: %w", hc.name, err)
				}
				hc.fallback = n
			}
			cols = append(cols, hc)
		}
		for name := range spec.Header.Fallback {
			if !contains(headerNames(cols), name) {
				return nil, fmt.Errorf("header.fallback.%s: not one of header.columns", name)
			}
		}
		if spec.Header.Search <= 0 {
			spec.Header.Search = 10
		}
		if spec.Header.Min <= 0 {
			spec.Header.Min = 1
		}
	}
	var fixed []headerCol
	if spec.Columns.Kind != 0 {
		if cols != nil {
			return nil, fmt.Errorf("use header or columns, not both")
		}
		pairs, err := orderedPairs(&spec.Columns)
		if err != nil {
			return nil, fmt.Errorf("columns: %w", err)
		}
		for _, p := range pairs {
			n, err := columnNumber(p[1])
			if err != nil {
				return nil, fmt.Errorf("columns.%s: %w", p[0], err)
			}
			fixed = append(fixed, headerCol{name: p[0], fallback: n})
		}
	}
	search, min := 0, 0
	if spec.Header != nil {
		search, min = spec.Header.Search, spec.Header.Min
	}
	return readStep(readOpts{input: spec.Input, sheet: sheetName, index: sheetIndex, header: cols, fixed: fixed, start: spec.Start,
		trim: spec.Trim, skipBlank: spec.SkipBlank, rowNumber: spec.RowNumber, search: search, min: min, empty: spec.Empty}), nil
}

func headerNames(cols []headerCol) []string {
	var out []string
	for _, c := range cols {
		out = append(out, c.name)
	}
	return out
}

// columnNumber reads "A".."XFD" or a 1-based number as a 0-based index.
func columnNumber(s string) (int, error) {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n - 1, nil
	}
	n, err := excelize.ColumnNameToNumber(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a column letter or number", s)
	}
	return n - 1, nil
}

type readOpts struct {
	input, sheet       string
	index              int
	header, fixed      []headerCol
	start, search, min int
	trim, skipBlank    bool
	rowNumber, empty   string
}

func readStep(o readOpts) Runner {
	input, header, fixed, start, trim, skipBlank, rowNumber := o.input, o.header, o.fixed, o.start, o.trim, o.skipBlank, o.rowNumber
	return func(_ context.Context, s *State) error {
		grid, name, err := readGrid(s.Inputs[input], o.sheet, o.index)
		if o.empty != "" && (errors.Is(err, errNoSheets) || err == nil && len(grid) == 0) {
			return errors.New(o.empty)
		}
		if err != nil {
			return err
		}
		var cols []headerCol
		first := 1 // 0-based first data row
		switch {
		case header != nil:
			cols = append([]headerCol(nil), header...)
			row, matched := findHeader(grid, cols, o.search, o.min)
			if row >= 0 {
				s.rt.Log.Printf("%s: header found at row %d (%d columns matched).", input, row+1, matched)
				first = row + 1
			} else {
				s.rt.Log.Printf("%s: no header row found; using the fallback column positions.", input)
			}
		case fixed != nil:
			cols = fixed
			first = 0
		default:
			if len(grid) == 0 {
				return fmt.Errorf("%s: sheet %q is empty", input, name)
			}
			for i, h := range grid[0] {
				if strings.TrimSpace(h) != "" {
					cols = append(cols, headerCol{name: h, fallback: i})
				}
			}
			if len(cols) == 0 {
				return fmt.Errorf("%s: sheet %q: header row is empty", input, name)
			}
		}
		if start > 0 {
			first = start - 1
		}
		t := &Table{}
		for _, c := range cols {
			t.Columns = append(t.Columns, c.name)
		}
		if rowNumber != "" {
			t.Columns = append(t.Columns, rowNumber)
		}
		for ri := first; ri < len(grid); ri++ {
			r := make([]any, len(t.Columns))
			any := false
			for i, c := range cols {
				v := ""
				if c.fallback >= 0 && c.fallback < len(grid[ri]) {
					v = grid[ri][c.fallback]
				}
				if trim {
					v = jsx.Trim(v)
				}
				r[i] = v
				any = any || v != ""
			}
			if skipBlank && !any {
				continue
			}
			if rowNumber != "" {
				r[len(cols)] = float64(ri + 1)
			}
			t.Rows = append(t.Rows, r)
		}
		s.Cur = t
		return t.validate()
	}
}

// findHeader looks for the first row (within search) where at least min
// columns find one of their titles; matched columns then point at the cell
// that matched. It returns -1 when no row qualifies.
func findHeader(grid [][]string, cols []headerCol, search, min int) (int, int) {
	for r := 0; r < len(grid) && r < search; r++ {
		pos := make([]int, len(cols))
		n := 0
		for i, c := range cols {
			pos[i] = -1
			for ci, v := range grid[r] {
				if c.titles[jsx.Norm(v)] {
					pos[i] = ci
					n++
					break
				}
			}
		}
		if n >= min {
			for i := range cols {
				if pos[i] >= 0 {
					cols[i].fallback = pos[i]
				}
			}
			return r, n
		}
	}
	return -1, 0
}

var errNoSheets = errors.New("workbook has no sheets")

func readGrid(path, name string, index int) ([][]string, string, error) {
	wb, err := sheet.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer wb.Close()
	names := wb.SheetNames()
	if len(names) == 0 {
		return nil, "", errNoSheets
	}
	if name == "" {
		if index >= len(names) {
			return nil, "", fmt.Errorf("workbook has %d sheet(s), no sheet %d", len(names), index+1)
		}
		name = names[index]
	} else if !contains(names, name) {
		return nil, "", fmt.Errorf("workbook has no sheet named %q", name)
	}
	grid, err := wb.Rows(name)
	return grid, name, err
}

func sheetNames(path string) ([]string, error) {
	wb, err := sheet.Open(path)
	if err != nil {
		return nil, err
	}
	defer wb.Close()
	return wb.SheetNames(), nil
}
