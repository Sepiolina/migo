package studio

import (
	"encoding/xml"
	"errors"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// The production input reader intentionally honors declared sheet ranges. The
// desktop XLSX viewer reads actual cells, including writer output beyond a stale
// dimension tag; it must not silently hide cells present in the exported file.
type previewXLSX struct{ *excelize.File }

func (b previewXLSX) SheetNames() []string                 { return b.GetSheetList() }
func (b previewXLSX) Rows(name string) ([][]string, error) { return b.GetRows(name) }

// window streams Excelize-formatted cells without building the full worksheet
// or retaining rows outside the requested page. Scanning to the end keeps
// physical row offsets and the actual last populated row independent of the
// workbook's declared dimension, which can be stale.
func (b previewXLSX) window(name string, offset, limit int) (page [][]string, total int, err error) {
	canonical, err := b.validateWorksheet(name)
	if err != nil {
		return nil, 0, err
	}
	if !canonical {
		rows, err := b.GetRows(name)
		if err != nil {
			return nil, 0, err
		}
		start := min(offset, len(rows))
		return rows[start:min(start+limit, len(rows))], len(rows), nil
	}
	rows, err := b.File.Rows(name)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		if closeErr := rows.Close(); err == nil {
			err = closeErr
		}
	}()
	page = make([][]string, 0, limit)
	for i := 0; rows.Next(); i++ {
		if i >= excelize.TotalRows {
			return nil, 0, errors.New("Worksheet exceeds Excel's row limit")
		}
		row, err := rows.Columns()
		if err != nil {
			return nil, 0, err
		}
		if len(row) > 0 {
			total = i + 1
		}
		if i >= offset && i-offset < limit {
			page = append(page, row)
		}
	}
	if err := rows.Error(); err != nil {
		return nil, 0, err
	}
	// GetRows semantics exclude trailing rows with no displayed values or
	// formulas, even when they carry formatting.
	return page[:min(len(page), max(0, total-offset))], total, nil
}

// Excelize's row iterator discards XML decoder errors. Validate the selected
// worksheet directly so malformed XML cannot silently produce a partial page.
// Defer unusual row layouts to GetRows, which applies Excelize's row repairs.
func (b previewXLSX) validateWorksheet(name string) (bool, error) {
	part := b.worksheetPart(name)
	if part == "" {
		return false, nil
	}
	input, err := openWorksheetPart(b, part, nil)
	if errors.Is(err, errMissingWorksheetPart) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer input.Close()
	decoder := xml.NewDecoder(input)
	decoder.CharsetReader = b.CharsetReader
	return validateWorksheetXML(decoder)
}

func (b previewXLSX) worksheetPart(name string) string {
	type relationship struct {
		ID     string `xml:"Id,attr"`
		Type   string `xml:"Type,attr"`
		Target string `xml:"Target,attr"`
	}
	readRelationships := func(part string) []relationship {
		var rels struct {
			Items []relationship `xml:"Relationship"`
		}
		data, ok := b.Pkg.Load(part)
		if !ok || xml.Unmarshal(data.([]byte), &rels) != nil {
			return nil
		}
		return rels.Items
	}
	var workbook string
	for _, rel := range readRelationships("_rels/.rels") {
		if strings.HasSuffix(rel.Type, "/officeDocument") {
			workbook = strings.TrimPrefix(rel.Target, "/")
			break
		}
	}
	if workbook == "" || b.WorkBook == nil {
		return ""
	}
	dir, base := path.Split(workbook)
	rels := readRelationships(dir + "_rels/" + base + ".rels")
	for _, sheet := range b.WorkBook.Sheets.Sheet {
		if sheet.Name != name {
			continue
		}
		for _, rel := range rels {
			if rel.ID != sheet.ID || !strings.HasSuffix(rel.Type, "/worksheet") {
				continue
			}
			target := strings.ReplaceAll(rel.Target, "\\", "/")
			if strings.HasPrefix(target, "/") {
				return strings.TrimPrefix(path.Clean(target), "/")
			}
			return path.Join(dir, target)
		}
	}
	return ""
}

func validateWorksheetXML(decoder *xml.Decoder) (bool, error) {
	const spreadsheet = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	const strictSpreadsheet = "http://purl.oclc.org/ooxml/spreadsheetml/main"
	canonical := true
	depth, row, col := 0, 0, 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return canonical, nil
		}
		if err != nil {
			return false, err
		}
		switch element := token.(type) {
		case xml.StartElement:
			depth++
			if depth == 1 && (element.Name.Local != "worksheet" || element.Name.Space != spreadsheet && element.Name.Space != strictSpreadsheet) {
				canonical = false
			}
			if element.Name.Local == "row" && depth == 3 {
				next := row + 1
				for _, attr := range element.Attr {
					if attr.Name.Local == "r" {
						next, err = strconv.Atoi(attr.Value)
						if err != nil {
							return false, err
						}
					}
				}
				if next < 0 || next > excelize.TotalRows {
					return false, errors.New("Worksheet exceeds Excel's row limit")
				}
				canonical = canonical && next > row
				row, col = next, 0
			}
			if element.Name.Local == "c" && depth == 4 {
				next := col + 1
				for _, attr := range element.Attr {
					if attr.Name.Local == "r" && attr.Value != "" {
						next, _, err = excelize.CellNameToCoordinates(attr.Value)
						if err != nil {
							return false, err
						}
					}
				}
				canonical = canonical && next > col
				col = next
			}
		case xml.EndElement:
			depth--
		}
	}
}
