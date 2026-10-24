package bom

import (
	"errors"

	"miservice/internal/jsx"
	"miservice/internal/logx"
	"miservice/internal/sheet"
)

// colKey identifies one logical BOM column.
type colKey int

const (
	kLevel colKey = iota
	kPartNumber
	kPartType
	kDescription
	kRev
	kQty
	kUOM
	kFindNum
	kRefDes
	kMfgName
	kMfrPN
	nColKeys
)

// headerSynonyms mirrors HEADER_SYNONYMS (matched case-insensitively). To
// accept another customer's column title, add it to the right line.
var headerSynonyms = [nColKeys][]string{
	kLevel:       {"level", "lvl", "item level"},
	kPartNumber:  {"part number", "part no", "item number", "internal pn", "p/n"},
	kPartType:    {"part type", "type", "category"},
	kDescription: {"description", "desc", "part description"},
	kRev:         {"rev", "revision", "item rev"},
	kQty:         {"bom.qty", "qty", "quantity", "bom qty"},
	kUOM:         {"bom.uom", "uom", "unit of measure", "unit"},
	kFindNum:     {"bom.find num", "find num", "find no", "find", "line item", "item"},
	kRefDes:      {"bom.ref des", "ref des", "reference designator", "designator", "refdes"},
	kMfgName:     {"manufacturers.mfg name", "mfg name", "manufacturer", "mfr name", "maker", "vendor"},
	kMfrPN: {"manufacturers.mfr. part number", "manufacturers.mfr part number", "mfr part number",
		"mfr. part number", "mpn", "mfr pn"},
}

// srcColsDefault mirrors SRC_COLS_DEFAULT (used when no header row matches).
var srcColsDefault = [nColKeys]int{
	kLevel: 0, kPartNumber: 1, kPartType: 2, kDescription: 3, kRev: 8, kQty: 25,
	kUOM: 26, kFindNum: 28, kRefDes: 29, kMfgName: 51, kMfrPN: 52,
}

const (
	headerSearchRows = 15 // the header must be in the first 15 rows
	headerMinMatches = 4  // ... and match at least 4 known columns
)

// ErrEmptyBOM is the "BOM file has no rows" error of readBom.
var ErrEmptyBOM = errors.New("The selected BOM file is empty.")

// ReadBom ports readers.readBom: first sheet, header row located by
// synonyms (or the default column layout), blank rows skipped.
func ReadBom(path string, log logx.Logger) ([]*Row, error) {
	raw, err := firstSheetRows(path)
	if err != nil {
		return nil, err
	}
	headerRow, matched, colMap := findHeader(raw)
	if headerRow >= 0 {
		log.Printf("BOM Headers detected at row %d: matched %d columns.", headerRow+1, matched)
	} else {
		log.Print("No explicit BOM header row matched; falling back to standard column layout.")
	}
	start := 1
	if headerRow >= 0 {
		start = headerRow + 1
	}
	return parseBomRows(raw, start, colMap), nil
}

func firstSheetRows(path string) ([][]string, error) {
	wb, err := sheet.Open(path)
	if err != nil {
		return nil, err
	}
	defer wb.Close()
	names := wb.SheetNames()
	if len(names) == 0 {
		return nil, ErrEmptyBOM
	}
	raw, err := wb.Rows(names[0])
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, ErrEmptyBOM
	}
	return raw, nil
}

// findHeader returns the 0-based header row (-1 if none), how many columns it
// matched, and the column map (the defaults, overridden by what matched).
func findHeader(raw [][]string) (headerRow, matchedCount int, colMap [nColKeys]int) {
	headerRow = -1
	colMap = srcColsDefault
	limit := headerSearchRows
	if len(raw) < limit {
		limit = len(raw)
	}
	for r := 0; r < limit; r++ {
		norms := make([]string, len(raw[r]))
		for i, v := range raw[r] {
			norms[i] = jsx.Norm(v)
		}
		var matched [nColKeys]int
		n := 0
		for k := colKey(0); k < nColKeys; k++ {
			matched[k] = -1
			for ci, v := range norms {
				if inNormSynonyms(headerSynonyms[k], v) {
					matched[k] = ci
					n++
					break
				}
			}
		}
		if n >= headerMinMatches {
			headerRow, matchedCount = r, n
			for k := colKey(0); k < nColKeys; k++ {
				if matched[k] >= 0 {
					colMap[k] = matched[k]
				}
			}
			break
		}
	}
	return headerRow, matchedCount, colMap
}

func parseBomRows(raw [][]string, start int, colMap [nColKeys]int) []*Row {
	var rows []*Row
	for ri := start; ri < len(raw); ri++ {
		rd := raw[ri]
		var vals [nColKeys]string
		any := false
		for k := colKey(0); k < nColKeys; k++ {
			if ci := colMap[k]; ci < len(rd) {
				vals[k] = cellToStr(rd[ci])
			}
			if vals[k] != "" {
				any = true
			}
		}
		if !any {
			continue
		}
		rows = append(rows, &Row{
			Level: vals[kLevel], PartNumber: vals[kPartNumber], PartType: vals[kPartType],
			Description: vals[kDescription], Rev: vals[kRev], Qty: vals[kQty], UOM: vals[kUOM],
			FindNum: vals[kFindNum], RefDes: vals[kRefDes], MfgName: vals[kMfgName], MfrPN: vals[kMfrPN],
			OriginalIndex: ri + 1, OrigMfrPN: vals[kMfrPN], InScope: true,
		})
	}
	return rows
}

func inNormSynonyms(syns []string, normalized string) bool {
	for _, s := range syns {
		if jsx.Norm(s) == normalized {
			return true
		}
	}
	return false
}

// PriorityMap maps norm(Forth P/N) -> priority value; a nil value is JS null
// (the key still counts towards the "Loaded N Priority entries" log line).
type PriorityMap map[string]*float64

const (
	priorityFirstRow  = 167 // 168th row (0-indexed): where the data starts
	priorityPartCol   = 0   // Forth P/N
	priorityValueCol  = 12  // priority
	prioritySheetName = "Shortage"
)

// ReadPriority ports readers.readPriority.
func ReadPriority(path string, log logx.Logger) (PriorityMap, error) {
	wb, err := sheet.Open(path)
	if err != nil {
		return nil, err
	}
	defer wb.Close()
	found := false
	for _, n := range wb.SheetNames() {
		if n == prioritySheetName {
			found = true
		}
	}
	if !found {
		return nil, errors.New(`Priority workbook has no sheet named "Shortage"`)
	}
	raw, err := wb.Rows(prioritySheetName)
	if err != nil {
		return nil, err
	}
	pmap := PriorityMap{}
	for r := priorityFirstRow; r < len(raw); r++ {
		row := raw[r]
		var fpn, pr string
		if len(row) > priorityPartCol {
			fpn = row[priorityPartCol]
		}
		if len(row) > priorityValueCol {
			pr = row[priorityValueCol]
		}
		key := jsx.Norm(fpn)
		if key == "" {
			continue
		}
		pmap[key] = parsePriorityValue(pr)
	}
	log.Printf("Loaded %d Priority entries.", len(pmap))
	return pmap, nil
}

// parsePriorityValue: the 1-ish spellings mean P1; otherwise parseFloat(pr)
// || null (so 0 and non-numbers are nil).
func parsePriorityValue(pr string) *float64 {
	switch jsx.Norm(pr) {
	case "1", "1.0", "01", "P1", "01.0":
		one := 1.0
		return &one
	}
	if v, ok := jsx.ParseFloat(pr); ok && v != 0 {
		return &v
	}
	return nil
}
