package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestInspectWorkbookPageReadsSavedStyles(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "saved.xlsx")
	book := excelize.NewFile()
	defer book.Close()
	const sheet = "Sheet1"
	header, err := book.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Italic: true, Underline: "single", Family: "Calibri", Size: 10, Color: "0000FF"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"FFEE99"}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "top", WrapText: true},
		Border:    []excelize.Border{{Type: "bottom", Color: "EE1133", Style: 5}},
		NumFmt:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	customFormat := "#,##0.00"
	number, err := book.NewStyle(&excelize.Style{CustomNumFmt: &customFormat})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := book.NewSheet("Second"); err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{
		book.SetCellValue(sheet, "A1", "heading"),
		book.SetRowHeight(sheet, 1, 28),
		book.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}),
		book.SetCellValue(sheet, "A2", "00123"),
		book.SetCellValue(sheet, "B2", 123),
		book.SetCellValue(sheet, "C2", true),
		book.SetCellValue(sheet, "C3", 12.5),
		book.SetCellStyle(sheet, "C3", "C3", header),
		book.SetCellValue(sheet, "C4", 12345.5),
		book.SetCellStyle(sheet, "C4", "C4", number),
		book.SetCellStyle(sheet, "D12", "D12", header),
		book.SetColWidth(sheet, "C", "D", 23.5),
		book.SetColVisible(sheet, "D", false),
		book.SetSheetDimension(sheet, "A1:A1"),
		book.MergeCell("Second", "A1", "B1"),
		book.SetCellValue("Second", "A1", "other sheet"),
		book.SaveAs(file),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}

	read := func(p Params) WorkbookPage {
		t.Helper()
		p.Path, p.Workspace = "saved.xlsx", root
		reply := Handle(context.Background(), Request{Version: Version, Command: "inspectWorkbookPage", Params: p})
		if !reply.OK {
			t.Fatalf("inspection failed: %+v", reply.Error)
		}
		return reply.Result.(WorkbookPage)
	}
	page := read(Params{Offset: 2, ColumnOffset: 2, Limit: 22, ColumnLimit: 2})
	if !reflect.DeepEqual(page.Sheets, []string{sheet, "Second"}) || page.Sheet != sheet ||
		page.TotalRows != 12 || page.TotalColumns != 4 || page.Offset != 2 || page.ColumnOffset != 2 {
		t.Fatalf("incorrect sheet extents and coordinates: %+v", page)
	}
	if !reflect.DeepEqual(page.Widths, []float64{23.5, 0}) || !slices.Contains(page.Unsupported, "Hidden columns") {
		t.Fatalf("incorrect widths or hidden-column disclosure: %+v", page)
	}
	if len(page.Rows) != 10 || page.Rows[0][0] != "12.50" || page.Rows[1][0] != "12,345.50" || page.Rows[9][1] != "" ||
		page.StyleIDs[0][0] != header || page.StyleIDs[1][0] != number || page.StyleIDs[9][1] != header || len(page.Styles) != 3 {
		t.Fatalf("incorrect cell text, styles, or style dictionary: %+v", page)
	}
	wantStyle := WorkbookCellStyle{
		Bold: true, Italic: true, Underline: true, FontFamily: "Calibri", FontSize: 10,
		FontColor: "#0000FF", FillColor: "#FFEE99", Horizontal: "center", Vertical: "top", Wrap: true,
		Borders: map[string]WorkbookCellBorder{"bottom": {Style: 5, Color: "#EE1133"}},
	}
	if style := page.Styles[header]; !reflect.DeepEqual(style, wantStyle) {
		t.Fatalf("style = %+v, want %+v", style, wantStyle)
	}
	first := read(Params{Limit: 3, ColumnLimit: 3})
	if !reflect.DeepEqual(first.RowHeights, []float64{28, 15, 15}) || !slices.Contains(first.Unsupported, "Frozen panes") {
		t.Fatalf("incorrect saved row heights or frozen-pane disclosure: %+v", first)
	}
	if !reflect.DeepEqual(first.Rows[1], []string{"00123", "123", "TRUE"}) ||
		!reflect.DeepEqual(first.GeneralAlignment, []string{"", ".rc", ""}) {
		t.Fatalf("incorrect General alignment or display values: %+v", first)
	}
	offset := read(Params{Offset: 1, ColumnOffset: 1, Limit: 1, ColumnLimit: 2})
	if !reflect.DeepEqual(offset.GeneralAlignment, []string{"rc"}) {
		t.Fatalf("incorrect General alignment at page offsets: %+v", offset)
	}

	second := read(Params{Sheet: "Second"})
	if second.Rows[0][0] != "other sheet" || !slices.Contains(second.Unsupported, "Merged cells") {
		t.Fatalf("incorrect second sheet or merge disclosure: %+v", second)
	}
	outside := read(Params{Sheet: "Second", Offset: 99, ColumnOffset: 99})
	if !reflect.DeepEqual(outside.Rows, [][]string{{""}}) || len(outside.Widths) != 1 || outside.TotalRows == 100 {
		t.Fatalf("incorrect page beyond used range: %+v", outside)
	}
}

func TestInspectWorkbookPageReadsSparseDiskSpool(t *testing.T) {
	// A worksheet part larger than Excelize's 8 MiB memory threshold uses its
	// temporary file path. Reading the last cell must still stay page-sized.
	file := previewWithWorksheetXML(t, `<worksheet xmlns="`+workbookXMLNamespace+`"><dimension ref="A1"/><!--`+
		strings.Repeat("x", 9<<20)+`--><sheetData><row r="1"><c r="A1"><v>1</v></c></row>`+
		`<row r="1048576"><c r="XFD1048576"><v>42</v></c></row></sheetData></worksheet>`)
	page, err := inspectWorkbookPage(context.Background(), file, Request{Params: Params{
		Offset: excelize.TotalRows - 1, ColumnOffset: excelize.MaxColumns - 1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(page.Rows, [][]string{{"42"}}) || page.TotalRows != excelize.TotalRows || page.TotalColumns != excelize.MaxColumns {
		t.Fatalf("wrong sparse page: %+v", page)
	}
}

func TestInspectWorkbookPageEnforcesFrameAndWorkbookLimits(t *testing.T) {
	// Rails made of repeated characters compress in the file but JSON-escape
	// to six bytes per character. The next smaller page fits without clipping.
	const rowCount = 20
	file := filepath.Join(t.TempDir(), "large-values.xlsx")
	book := excelize.NewFile()
	defer book.Close()
	for index := 1; index <= rowCount; index++ {
		cell, _ := excelize.CoordinatesToCellName(1, index)
		if err := book.SetCellStr("Sheet1", cell, strings.Repeat("<", 30_000)); err != nil {
			t.Fatal(err)
		}
	}
	if err := book.SaveAs(file); err != nil {
		t.Fatal(err)
	}
	req := Request{Version: Version, ID: "page", Command: "inspectWorkbookPage", Params: Params{
		Workspace: filepath.Dir(file), Path: filepath.Base(file), Limit: rowCount, ColumnLimit: 1,
	}}
	send := func() []byte {
		t.Helper()
		request, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if err := Serve(context.Background(), bytes.NewReader(append(request, '\n')), &output); err != nil {
			t.Fatal(err)
		}
		if output.Len() > MaxFrameBytes {
			t.Fatalf("runner returned %d bytes", output.Len())
		}
		return output.Bytes()
	}
	if response := send(); !bytes.Contains(response, []byte(`"code":"response_too_large"`)) ||
		!bytes.Contains(response, []byte("response limit")) {
		t.Fatalf("oversize page should fail explicitly: %.300s", response)
	}
	req.Params.Limit = 2
	var response struct {
		OK     bool
		Result WorkbookPage
	}
	if err := json.Unmarshal(send(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || len(response.Result.Rows) != 2 || len(response.Result.Rows[0][0]) != 30_000 ||
		response.Result.TotalRows != rowCount {
		t.Fatal("smaller page should retain full values and sheet extents")
	}
	oversized := filepath.Join(req.Params.Workspace, "oversized.xlsx")
	disk, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := disk.Truncate(maxWorkbookBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := disk.Close(); err != nil {
		t.Fatal(err)
	}
	req.Params.Path = filepath.Base(oversized)
	if response := send(); !bytes.Contains(response, []byte("25 MiB")) {
		t.Fatalf("oversize workbook should be refused: %.300s", response)
	}
}

func TestInspectWorkbookPageUsesSavedFormulaResults(t *testing.T) {
	file := previewWithWorksheetXML(t, `<worksheet xmlns="`+workbookXMLNamespace+`"><sheetFormatPr defaultColWidth="14.5" defaultRowHeight="16.5"/>`+
		`<sheetData><row r="1"><c r="A1"><f>2+2</f><v>8</v></c><c r="B1"><f>3+3</f></c>`+
		`<c r="C1" t="inlineStr"><is><r><t>rich </t></r><r><t>text</t></r></is></c>`+
		`<c r="D1" t="e"><v>#N/A</v></c><c r="E1" t="d"><v>2026-01-01</v></c></row></sheetData>`+
		`<conditionalFormatting sqref="A1"><cfRule type="expression" priority="1"><formula>1</formula></cfRule></conditionalFormatting></worksheet>`)
	page, err := inspectWorkbookPage(context.Background(), file, Request{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(page.Rows, [][]string{{"8", "", "rich text", "#N/A", "2026-01-01"}}) ||
		!reflect.DeepEqual(page.Widths, []float64{14.5, 14.5, 14.5, 14.5, 14.5}) ||
		!reflect.DeepEqual(page.RowHeights, []float64{16.5}) ||
		!reflect.DeepEqual(page.GeneralAlignment, []string{"r..cr"}) ||
		!reflect.DeepEqual(page.Unsupported, []string{"Conditional formatting", "Formulas without saved values", "Rich text"}) {
		t.Fatalf("incorrect formula caches, rich text, or unsupported disclosures: %+v", page)
	}
}
