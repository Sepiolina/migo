package studio

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

const (
	defaultWorkbookRows    = 100
	defaultWorkbookColumns = 32
	maxWorkbookPageCells   = 32_000
	defaultWorkbookWidth   = 9.140625 // Excelize's default, in Excel column-width units.
	defaultWorkbookHeight  = 15       // Excel's default row height, in points.
	workbookXMLNamespace   = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
)

var errWorkbookPageTooLarge = errors.New("Workbook page exceeds the 1 MiB response limit. Request fewer rows or columns.")

type WorkbookPage struct {
	Sheets           []string                  `json:"sheets"`
	Sheet            string                    `json:"sheet"`
	Offset           int                       `json:"offset"`
	ColumnOffset     int                       `json:"columnOffset"`
	TotalRows        int                       `json:"totalRows"`
	TotalColumns     int                       `json:"totalColumns"`
	Rows             [][]string                `json:"rows"`
	StyleIDs         [][]int                   `json:"styleIds"`
	Styles           map[int]WorkbookCellStyle `json:"styles"`
	Widths           []float64                 `json:"widths"`
	RowHeights       []float64                 `json:"rowHeights,omitempty"`       // Points, relative to Offset.
	GeneralAlignment []string                  `json:"generalAlignment,omitempty"` // r: right, c: center, .: use style/default.
	Unsupported      []string                  `json:"unsupported"`
}

type WorkbookCellStyle struct {
	Bold       bool                          `json:"bold,omitempty"`
	Italic     bool                          `json:"italic,omitempty"`
	Underline  bool                          `json:"underline,omitempty"`
	FontColor  string                        `json:"fontColor,omitempty"`
	FillColor  string                        `json:"fillColor,omitempty"`
	FontSize   float64                       `json:"fontSize,omitempty"`
	FontFamily string                        `json:"fontFamily,omitempty"`
	Horizontal string                        `json:"horizontal,omitempty"`
	Vertical   string                        `json:"vertical,omitempty"`
	Wrap       bool                          `json:"wrap,omitempty"`
	Borders    map[string]WorkbookCellBorder `json:"borders,omitempty"`
}

type WorkbookCellBorder struct {
	Style int    `json:"style"`
	Color string `json:"color,omitempty"`
}

type worksheetPageCell struct {
	Style   *int      `xml:"s,attr,omitempty"`
	Type    string    `xml:"t,attr,omitempty"`
	Value   string    `xml:"v"`
	Formula *struct{} `xml:"f"`
	Inline  *struct {
		Runs []struct{} `xml:"r"`
	} `xml:"is"`
	Contents string `xml:",innerxml"`
}

type worksheetPageColumn struct {
	First  int      `xml:"min,attr"`
	Last   int      `xml:"max,attr"`
	Width  *float64 `xml:"width,attr"`
	Hidden bool     `xml:"hidden,attr"`
	Style  int      `xml:"style,attr"`
}

type worksheetPageRow struct {
	Number *int     `xml:"r,attr"`
	Style  int      `xml:"s,attr"`
	Hidden bool     `xml:"hidden,attr"`
	Height *float64 `xml:"ht,attr"`
}

type worksheetPageScan struct {
	cells         []map[int]worksheetPageCell
	rowStyles     []int
	rowHeights    []float64
	columns       []worksheetPageColumn
	defaultWidth  float64
	defaultHeight float64
	unsupported   map[string]bool
}

func inspectWorkbookPage(ctx context.Context, file string, req Request) (WorkbookPage, error) {
	p := req.Params
	page := WorkbookPage{Offset: p.Offset, ColumnOffset: p.ColumnOffset}
	if p.Offset < 0 || p.Offset >= excelize.TotalRows || p.ColumnOffset < 0 || p.ColumnOffset >= excelize.MaxColumns {
		return page, errors.New("Workbook page offsets are outside the Excel grid")
	}
	if p.Limit < 0 || p.Limit > 1000 || p.ColumnLimit < 0 || p.ColumnLimit > 1000 {
		return page, errors.New("Workbook page row and column limits must be between 0 and 1000")
	}
	if p.Limit == 0 {
		p.Limit = defaultWorkbookRows
	}
	if p.ColumnLimit == 0 {
		p.ColumnLimit = defaultWorkbookColumns
	}
	p.Limit = min(p.Limit, excelize.TotalRows-p.Offset)
	p.ColumnLimit = min(p.ColumnLimit, excelize.MaxColumns-p.ColumnOffset)
	if p.Limit*p.ColumnLimit > maxWorkbookPageCells {
		return page, fmt.Errorf("Workbook pages may contain at most %d cells", maxWorkbookPageCells)
	}
	if !strings.EqualFold(filepath.Ext(file), ".xlsx") {
		return page, errors.New("Styled workbook preview requires an XLSX file")
	}
	inputFile, err := openWorkbookFile(file, maxWorkbookBytes)
	if err != nil {
		return page, err
	}
	defer inputFile.Close()
	archive, size, err := workbookArchive(inputFile, maxWorkbookBytes)
	if err != nil {
		return page, err
	}
	if archive == nil {
		return page, errors.New("Styled workbook preview requires an XLSX archive")
	}
	fileBook, err := excelize.OpenReader(io.NewSectionReader(inputFile, 0, size), excelize.Options{
		UnzipSizeLimit: maxWorkbookExpandedBytes, UnzipXMLSizeLimit: 8 << 20,
	})
	if err != nil {
		if fileBook != nil {
			fileBook.Close()
		}
		return page, err
	}
	defer fileBook.Close()
	book := previewXLSX{fileBook}
	page.Sheets = book.GetSheetList()
	if len(page.Sheets) == 0 {
		return page, errors.New("Workbook has no sheets")
	}
	page.Sheet = p.Sheet
	if page.Sheet == "" {
		page.Sheet = page.Sheets[0]
	} else if !slices.Contains(page.Sheets, page.Sheet) {
		return page, fmt.Errorf("Unknown sheet %q", page.Sheet)
	}
	part := book.worksheetPart(page.Sheet)
	if part == "" {
		return page, fmt.Errorf("Sheet %q has no worksheet data", page.Sheet)
	}
	scan := worksheetPageScan{
		cells:        make([]map[int]worksheetPageCell, p.Limit),
		rowStyles:    make([]int, p.Limit),
		rowHeights:   make([]float64, p.Limit),
		columns:      make([]worksheetPageColumn, p.ColumnLimit),
		defaultWidth: defaultWorkbookWidth,
		unsupported:  make(map[string]bool),
	}
	// Excelize spools large worksheet parts. Use the same open file for
	// those parts so an atomic replacement cannot mix workbook versions.
	input, err := openWorksheetPart(book, part, archive)
	if err != nil {
		return page, err
	}
	decoder := xml.NewDecoder(input)
	decoder.CharsetReader = book.CharsetReader
	err = scan.read(ctx, decoder, p, &page)
	closeErr := input.Close()
	if err != nil {
		return page, err
	}
	if closeErr != nil {
		return page, closeErr
	}
	if err := scan.project(ctx, book, part, req, &page); err != nil {
		return page, err
	}
	return page, nil
}

var errMissingWorksheetPart = errors.New("Missing worksheet part")

func openWorksheetPart(book previewXLSX, part string, archive *zip.Reader) (io.ReadCloser, error) {
	if data, ok := book.Pkg.Load(part); ok {
		return io.NopCloser(bytes.NewReader(data.([]byte))), nil
	}
	if archive != nil {
		return openWorksheetArchivePart(archive, part)
	}
	file, err := zip.OpenReader(book.Path)
	if err != nil {
		return nil, err
	}
	input, err := openWorksheetArchivePart(&file.Reader, part)
	if err != nil {
		file.Close()
		return nil, err
	}
	return worksheetPartReader{input, file}, nil
}

func openWorksheetArchivePart(archive *zip.Reader, part string) (io.ReadCloser, error) {
	for _, entry := range archive.File {
		if strings.ReplaceAll(entry.Name, "\\", "/") != part {
			continue
		}
		return entry.Open()
	}
	return nil, fmt.Errorf("%w %q", errMissingWorksheetPart, part)
}

type worksheetPartReader struct {
	io.ReadCloser
	archive *zip.ReadCloser
}

func (r worksheetPartReader) Close() error {
	return errors.Join(r.ReadCloser.Close(), r.archive.Close())
}

func (scan *worksheetPageScan) read(ctx context.Context, decoder *xml.Decoder, p Params, page *WorkbookPage) error {
	var row, column, depth int
	var inData, inColumns, inViews, inView, worksheet bool
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			if !worksheet {
				return errors.New("Missing worksheet XML")
			}
			return nil
		}
		if err != nil {
			return err
		}
		switch element := token.(type) {
		case xml.StartElement:
			depth++
			switch depth {
			case 1:
				if worksheet || element.Name.Local != "worksheet" {
					return errors.New("Invalid worksheet XML root")
				}
				worksheet = true
			case 2:
				inData = element.Name.Local == "sheetData"
				inColumns = element.Name.Local == "cols"
				inViews = element.Name.Local == "sheetViews"
				scan.noteFeature(element.Name.Local)
				if element.Name.Local == "sheetFormatPr" {
					for _, attr := range element.Attr {
						if attr.Name.Local == "defaultColWidth" {
							width, err := strconv.ParseFloat(attr.Value, 64)
							if err != nil || !validWorkbookWidth(width) {
								return errors.New("Invalid default worksheet column width")
							}
							scan.defaultWidth = width
						}
						if attr.Name.Local == "defaultRowHeight" {
							height, err := parseWorkbookHeight(attr.Value)
							if err != nil {
								return err
							}
							scan.defaultHeight = height
						}
						if attr.Name.Local == "zeroHeight" && (attr.Value == "1" || attr.Value == "true") {
							scan.unsupported["Hidden rows"] = true
						}
					}
				}
			case 3:
				inView = inViews && element.Name.Local == "sheetView"
				switch {
				case inColumns && element.Name.Local == "col":
					var column worksheetPageColumn
					if err := decoder.DecodeElement(&column, &element); err != nil {
						return err
					}
					depth--
					if err := scan.addColumn(column, p, page); err != nil {
						return err
					}
				case inData && element.Name.Local == "row":
					metadata, err := readWorksheetPageRow(element.Attr)
					if err != nil {
						return err
					}
					row++
					if metadata.Number != nil {
						row = *metadata.Number
					}
					if row < 0 || row > excelize.TotalRows {
						return errors.New("Worksheet exceeds Excel's row limit")
					}
					column = 0
					if metadata.Hidden || metadata.Height != nil && *metadata.Height == 0 {
						scan.unsupported["Hidden rows"] = true
					}
					if metadata.Style != 0 {
						page.TotalRows = max(page.TotalRows, row)
						page.TotalColumns = max(page.TotalColumns, 1)
					}
					if i := row - p.Offset - 1; i >= 0 && i < p.Limit {
						scan.rowStyles[i] = metadata.Style
						if metadata.Height != nil {
							scan.rowHeights[i] = *metadata.Height
						}
					}
				}
			case 4:
				if inView && element.Name.Local == "pane" {
					for _, attr := range element.Attr {
						if attr.Name.Local == "state" && (attr.Value == "frozen" || attr.Value == "frozenSplit") {
							scan.unsupported["Frozen panes"] = true
						}
					}
				}
				if !inData || element.Name.Local != "c" {
					continue
				}
				column++
				actualRow := row
				for _, attr := range element.Attr {
					if attr.Name.Local == "r" && attr.Value != "" {
						column, actualRow, err = excelize.CellNameToCoordinates(attr.Value)
						if err != nil {
							return err
						}
					}
				}
				if actualRow < 1 || actualRow > excelize.TotalRows || column < 1 || column > excelize.MaxColumns {
					return errors.New("Cell is outside the Excel grid")
				}
				page.TotalRows = max(page.TotalRows, actualRow)
				page.TotalColumns = max(page.TotalColumns, column)
				rowIndex, colIndex := actualRow-p.Offset-1, column-p.ColumnOffset-1
				if rowIndex < 0 || rowIndex >= p.Limit || colIndex < 0 || colIndex >= p.ColumnLimit {
					if err := decoder.Skip(); err != nil {
						return err
					}
				} else {
					var cell worksheetPageCell
					if err := decoder.DecodeElement(&cell, &element); err != nil {
						return err
					}
					if err := scan.addCell(rowIndex, colIndex, cell); err != nil {
						return err
					}
				}
				depth--
			}
		case xml.EndElement:
			if depth == 2 {
				inData, inColumns, inViews = false, false, false
			}
			if depth == 3 {
				inView = false
			}
			depth--
		}
	}
}

func readWorksheetPageRow(attrs []xml.Attr) (worksheetPageRow, error) {
	var metadata worksheetPageRow
	for _, attr := range attrs {
		var err error
		switch attr.Name.Local {
		case "r":
			number, parseErr := strconv.Atoi(attr.Value)
			metadata.Number, err = &number, parseErr
		case "s":
			metadata.Style, err = strconv.Atoi(attr.Value)
		case "hidden":
			metadata.Hidden, err = strconv.ParseBool(attr.Value)
		case "ht":
			height, parseErr := parseWorkbookHeight(attr.Value)
			metadata.Height, err = &height, parseErr
		}
		if err != nil {
			return metadata, err
		}
	}
	return metadata, nil
}

func parseWorkbookHeight(value string) (float64, error) {
	height, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(height) || height < 0 || height > excelize.MaxRowHeight {
		return 0, errors.New("Invalid worksheet row height")
	}
	return height, nil
}

func validWorkbookWidth(width float64) bool {
	return !math.IsNaN(width) && width >= 0 && width <= 255
}

func (scan *worksheetPageScan) addColumn(column worksheetPageColumn, p Params, page *WorkbookPage) error {
	if column.First < 1 || column.Last < column.First || column.Last > excelize.MaxColumns {
		return errors.New("Invalid worksheet column range")
	}
	if column.Width != nil && !validWorkbookWidth(*column.Width) {
		return errors.New("Invalid worksheet column width")
	}
	if column.Hidden || column.Width != nil && *column.Width == 0 {
		scan.unsupported["Hidden columns"] = true
	}
	if column.Style != 0 {
		page.TotalColumns = max(page.TotalColumns, column.Last)
		page.TotalRows = max(page.TotalRows, 1)
	}
	for i := max(0, column.First-p.ColumnOffset-1); i < min(p.ColumnLimit, column.Last-p.ColumnOffset); i++ {
		scan.columns[i] = column
	}
	return nil
}

func (scan *worksheetPageScan) addCell(row, col int, cell worksheetPageCell) error {
	if cell.Type == "s" && cell.Value != "" {
		index, err := strconv.Atoi(strings.TrimSpace(cell.Value))
		if err != nil || index < 0 {
			return errors.New("Invalid shared-string index in worksheet cell")
		}
	}
	if cell.Inline != nil && len(cell.Inline.Runs) != 0 {
		scan.unsupported["Rich text"] = true
	}
	if cell.Formula != nil && cell.Value == "" {
		scan.unsupported["Formulas without saved values"] = true
	}
	if scan.cells[row] == nil {
		scan.cells[row] = make(map[int]worksheetPageCell)
	}
	// Excelize preserves the first cell at an address if duplicate records occur.
	if _, exists := scan.cells[row][col]; !exists {
		scan.cells[row][col] = cell
	}
	return nil
}

func (cell worksheetPageCell) generalAlignment() byte {
	if cell.Value == "" {
		return '.'
	}
	switch cell.Type {
	case "", "n", "d":
		return 'r'
	case "b", "e":
		return 'c'
	default:
		return '.'
	}
}

func (scan *worksheetPageScan) noteFeature(element string) {
	switch element {
	case "mergeCells":
		scan.unsupported["Merged cells"] = true
	case "conditionalFormatting":
		scan.unsupported["Conditional formatting"] = true
	case "drawing", "legacyDrawing", "picture", "oleObjects":
		scan.unsupported["Drawings, images, and objects"] = true
	case "tableParts":
		scan.unsupported["Table styles"] = true
	case "extLst":
		scan.unsupported["Worksheet extensions"] = true
	}
}

// Excelize formats cached workbook values against the workbook's own styles,
// shared strings and date system. Feed it just the visible cells, rebased to
// the page origin, so sparse or very wide worksheets cannot force it to
// allocate the full sheet. The compact XML only exists in memory.
func (scan *worksheetPageScan) project(ctx context.Context, book previewXLSX, part string, req Request, page *WorkbookPage) error {
	rowCount := max(1, min(len(scan.cells), page.TotalRows-page.Offset))
	columnCount := max(1, min(len(scan.columns), page.TotalColumns-page.ColumnOffset))
	page.Widths = make([]float64, columnCount)
	page.Rows = make([][]string, rowCount)
	page.StyleIDs = make([][]int, rowCount)
	page.Styles = make(map[int]WorkbookCellStyle)
	page.Unsupported = []string{}
	rowHeights := scan.rowHeights[:rowCount]
	if scan.defaultHeight > 0 || slices.ContainsFunc(rowHeights, func(height float64) bool { return height > 0 }) {
		defaultHeight := scan.defaultHeight
		if defaultHeight == 0 {
			defaultHeight = defaultWorkbookHeight
		}
		for i, height := range rowHeights {
			if height == 0 {
				rowHeights[i] = defaultHeight
			}
		}
		page.RowHeights = rowHeights
	}
	for c, column := range scan.columns[:columnCount] {
		switch {
		case column.Hidden:
			page.Widths[c] = 0
		case column.Width != nil:
			page.Widths[c] = *column.Width
		default:
			page.Widths[c] = scan.defaultWidth
		}
	}

	var compact bytes.Buffer
	compact.WriteString(`<worksheet xmlns="` + workbookXMLNamespace + `"><sheetData>`)
	payloadBytes := 0
	for r, cells := range scan.cells[:rowCount] {
		page.Rows[r] = make([]string, columnCount)
		page.StyleIDs[r] = make([]int, columnCount)
		var alignment strings.Builder
		if len(cells) != 0 {
			fmt.Fprintf(&compact, "<row r=\"%d\">", r+1)
		}
		for c, column := range scan.columns[:columnCount] {
			styleID := column.Style
			if scan.rowStyles[r] != 0 {
				styleID = scan.rowStyles[r]
			}
			cell, exists := cells[c]
			if exists && cell.Style != nil {
				styleID = *cell.Style
			}
			if _, ok := page.Styles[styleID]; !ok {
				style, err := scan.style(book.File, styleID)
				if err != nil {
					return err
				}
				if len(style.FontFamily)+len(style.Horizontal)+len(style.Vertical) > MaxFrameBytes-payloadBytes {
					return errWorkbookPageTooLarge
				}
				encoded, err := json.Marshal(style)
				if err != nil {
					return err
				}
				payloadBytes += len(encoded)
				if payloadBytes > MaxFrameBytes {
					return errWorkbookPageTooLarge
				}
				page.Styles[styleID] = style
			}
			page.StyleIDs[r][c] = styleID
			if !exists {
				continue
			}
			if horizontal := page.Styles[styleID].Horizontal; horizontal == "" || horizontal == "general" {
				if direction := cell.generalAlignment(); direction != '.' {
					for alignment.Len() < c {
						alignment.WriteByte('.')
					}
					alignment.WriteByte(direction)
				}
			}
			address, err := excelize.CoordinatesToCellName(c+1, r+1)
			if err != nil {
				return err
			}
			fmt.Fprintf(&compact, "<c r=\"%s\" s=\"%d\"", address, styleID)
			if cell.Type != "" {
				compact.WriteString(` t="`)
				if err := xml.EscapeText(&compact, []byte(cell.Type)); err != nil {
					return err
				}
				compact.WriteByte('"')
			}
			compact.WriteByte('>')
			compact.WriteString(cell.Contents)
			compact.WriteString("</c>")
		}
		if len(cells) != 0 {
			compact.WriteString("</row>")
		}
		if alignment.Len() != 0 {
			if page.GeneralAlignment == nil {
				page.GeneralAlignment = make([]string, rowCount)
			}
			page.GeneralAlignment[r] = alignment.String()
		}
	}
	compact.WriteString("</sheetData></worksheet>")
	book.Pkg.Store(part, compact.Bytes())

	for row, cells := range scan.cells[:rowCount] {
		if err := ctx.Err(); err != nil {
			return err
		}
		for column, cell := range cells {
			if cell.Value == "" && cell.Inline == nil {
				continue
			}
			address, err := excelize.CoordinatesToCellName(column+1, row+1)
			if err != nil {
				return err
			}
			value, err := book.GetCellValue(page.Sheet, address)
			if err != nil {
				return err
			}
			if cell.Type == "s" && book.SharedStrings != nil {
				index, _ := strconv.Atoi(strings.TrimSpace(cell.Value))
				if index < len(book.SharedStrings.SI) && len(book.SharedStrings.SI[index].R) > 0 {
					scan.unsupported["Rich text"] = true
				}
			}
			if len(value) > MaxFrameBytes-payloadBytes {
				return errWorkbookPageTooLarge
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				return err
			}
			payloadBytes += len(encoded)
			if payloadBytes > MaxFrameBytes {
				return errWorkbookPageTooLarge
			}
			page.Rows[row][column] = value
		}
	}
	for feature := range scan.unsupported {
		page.Unsupported = append(page.Unsupported, feature)
	}
	slices.Sort(page.Unsupported)

	encoded, err := json.Marshal(Response{Version: Version, ID: req.ID, OK: true, Result: page})
	if err != nil {
		return err
	}
	if len(encoded)+1 > MaxFrameBytes {
		return errWorkbookPageTooLarge
	}
	return nil
}

func (scan *worksheetPageScan) style(book *excelize.File, id int) (WorkbookCellStyle, error) {
	var style WorkbookCellStyle
	saved := book.Styles
	if id == 0 && (saved == nil || saved.CellXfs == nil || len(saved.CellXfs.Xf) == 0) {
		return style, nil
	}
	if id < 0 || saved == nil || saved.CellXfs == nil || id >= len(saved.CellXfs.Xf) {
		return style, fmt.Errorf("Invalid saved cell style %d", id)
	}
	xf := saved.CellXfs.Xf[id]
	alignment, fontID, fillID, borderID := xf.Alignment, xf.FontID, xf.FillID, xf.BorderID
	if xf.XfID != nil && saved.CellStyleXfs != nil && *xf.XfID >= 0 && *xf.XfID < len(saved.CellStyleXfs.Xf) {
		base := saved.CellStyleXfs.Xf[*xf.XfID]
		if alignment == nil || xf.ApplyAlignment != nil && !*xf.ApplyAlignment {
			alignment = base.Alignment
		}
		if fontID == nil || xf.ApplyFont != nil && !*xf.ApplyFont {
			fontID = base.FontID
		}
		if fillID == nil || xf.ApplyFill != nil && !*xf.ApplyFill {
			fillID = base.FillID
		}
		if borderID == nil || xf.ApplyBorder != nil && !*xf.ApplyBorder {
			borderID = base.BorderID
		}
	}
	if alignment != nil {
		style.Horizontal = alignment.Horizontal
		style.Vertical = alignment.Vertical
		style.Wrap = alignment.WrapText
		if alignment.Indent != 0 || alignment.RelativeIndent != 0 || alignment.TextRotation != 0 ||
			alignment.ShrinkToFit || alignment.JustifyLastLine || alignment.ReadingOrder != 0 {
			scan.unsupported["Advanced text alignment"] = true
		}
		switch alignment.Horizontal {
		case "fill", "justify", "centerContinuous", "distributed":
			scan.unsupported["Advanced text alignment"] = true
		}
		switch alignment.Vertical {
		case "distributed", "justify":
			scan.unsupported["Advanced text alignment"] = true
		}
	}
	if fontID != nil && saved.Fonts != nil {
		if *fontID < 0 || *fontID >= len(saved.Fonts.Font) {
			return style, fmt.Errorf("Invalid font for saved cell style %d", id)
		}
		if font := saved.Fonts.Font[*fontID]; font != nil {
			style.Bold = font.B != nil && font.B.Value()
			style.Italic = font.I != nil && font.I.Value()
			if font.U != nil {
				underline := font.U.Value()
				style.Underline = underline != "none"
				if underline != "" && underline != "none" && underline != "single" {
					scan.unsupported["Advanced font effects"] = true
				}
			}
			if font.Name != nil {
				style.FontFamily = font.Name.Value()
			}
			if font.Sz != nil {
				style.FontSize = font.Sz.Value()
			}
			if font.Color != nil && !font.Color.Auto {
				style.FontColor = workbookColor(book, font.Color.RGB, font.Color.Indexed, font.Color.Theme, font.Color.Tint)
			}
			if (font.Strike != nil && font.Strike.Value()) || (font.Shadow != nil && font.Shadow.Value()) ||
				(font.Outline != nil && font.Outline.Value()) || (font.Condense != nil && font.Condense.Value()) ||
				(font.Extend != nil && font.Extend.Value()) {
				scan.unsupported["Advanced font effects"] = true
			}
		}
	}
	if fillID != nil && saved.Fills != nil {
		if *fillID < 0 || *fillID >= len(saved.Fills.Fill) {
			return style, fmt.Errorf("Invalid fill for saved cell style %d", id)
		}
		if fill := saved.Fills.Fill[*fillID]; fill != nil {
			if fill.GradientFill != nil {
				scan.unsupported["Gradient fills"] = true
			}
			if pattern := fill.PatternFill; pattern != nil {
				switch pattern.PatternType {
				case "solid":
					if color := pattern.FgColor; color != nil && !color.Auto {
						style.FillColor = workbookColor(book, color.RGB, color.Indexed, color.Theme, color.Tint)
					}
				case "", "none":
				default:
					scan.unsupported["Pattern fills"] = true
				}
			}
		}
	}
	if borderID != nil && saved.Borders != nil {
		if *borderID < 0 || *borderID >= len(saved.Borders.Border) {
			return style, fmt.Errorf("Invalid border for saved cell style %d", id)
		}
		if border := saved.Borders.Border[*borderID]; border != nil {
			style.Borders = map[string]WorkbookCellBorder{}
			if border.DiagonalUp || border.DiagonalDown {
				scan.unsupported["Diagonal borders"] = true
			}
			for _, side := range []string{"left", "right", "top", "bottom"} {
				line := border.Left
				switch side {
				case "right":
					line = border.Right
				case "top":
					line = border.Top
				case "bottom":
					line = border.Bottom
				}
				weight := slices.Index(workbookBorderStyles, line.Style)
				if line.Style != "" && weight < 0 {
					scan.unsupported["Advanced borders"] = true
				}
				if weight > 0 {
					edge := WorkbookCellBorder{Style: weight}
					if color := line.Color; color != nil && !color.Auto {
						edge.Color = workbookColor(book, color.RGB, color.Indexed, color.Theme, color.Tint)
					}
					style.Borders[side] = edge
				}
			}
		}
	}
	return style, nil
}

var workbookBorderStyles = []string{
	"none", "thin", "medium", "dashed", "dotted", "thick", "double", "hair", "mediumDashed",
	"dashDot", "mediumDashDot", "dashDotDot", "mediumDashDotDot", "slantDashDot",
}

func workbookColor(book *excelize.File, rgb string, indexed int, theme *int, tint float64) string {
	if indexed < 0 {
		return ""
	}
	if len(rgb) == 8 {
		rgb = rgb[2:]
	}
	if theme != nil && *theme >= 0 && *theme < len(defaultWorkbookTheme) {
		rgb = book.GetBaseColor(defaultWorkbookTheme[*theme], indexed, theme)
	} else if len(rgb) != 6 {
		rgb = book.GetBaseColor(rgb, indexed, nil)
	}
	if len(rgb) != 6 {
		return ""
	}
	if _, err := strconv.ParseUint(rgb, 16, 32); err != nil {
		return ""
	}
	return "#" + strings.ToUpper(excelize.ThemeColor(rgb, tint)[2:])
}

var defaultWorkbookTheme = []string{
	"FFFFFF", "000000", "EEECE1", "1F497D", "4F81BD", "C0504D", "9BBB59", "8064A2",
	"4BACC6", "F79646", "0000FF", "800080",
}
