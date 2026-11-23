package studio

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestXLSXPreviewWindow(t *testing.T) {
	file := filepath.Join(t.TempDir(), "window.xlsx")
	book := excelize.NewFile()
	defer book.Close()
	const sheet = "Sheet1"
	style, err := book.NewStyle(&excelize.Style{NumFmt: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{
		book.SetCellValue(sheet, "A1", "heading"),
		book.SetCellValue(sheet, "C3", 12.5),
		book.SetCellStyle(sheet, "C3", "C3", style),
		book.SetCellFormula(sheet, "B5", "1+1"),
		book.SetCellStyle(sheet, "D12", "D12", style),
		book.SetSheetDimension(sheet, "A1:A1"),
		book.SaveAs(file),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name      string
		offset    int
		limit     int
		rows      [][]string
		truncated bool
	}{
		{"first", 0, 3, [][]string{{"heading"}, nil, {"", "", "12.50"}}, true},
		{"tail", 3, 100, [][]string{nil, {"", ""}}, false},
		{"past end", 9, 3, [][]string{}, false},
		{"default limit", 0, 0, [][]string{{"heading"}, nil, {"", "", "12.50"}, nil, {"", ""}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := inspect(file, Params{Offset: tt.offset, Limit: tt.limit})
			if err != nil {
				t.Fatal(err)
			}
			want := Preview{Sheets: []string{sheet}, Sheet: sheet, Rows: tt.rows, Offset: tt.offset, TotalRows: 5, Truncated: tt.truncated}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("preview = %+v, want %+v", got, want)
			}
		})
	}
}

func TestXLSXPreviewRejectsBrokenWorksheetXML(t *testing.T) {
	file := previewWithWorksheetXML(t, `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1"><v>123</v></c></row></sheetData><sheetViews></worksheet>`)
	if _, err := inspect(file, Params{Limit: 1}); err == nil {
		t.Fatal("preview succeeded even though worksheet XML was malformed after the page")
	}
}

func TestXLSXPreviewRetainsExcelizeRowRepairs(t *testing.T) {
	file := previewWithWorksheetXML(t, `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="2"><c r="A2"><v>123</v></c></row><row r="2"><c r="C2"><v>456</v></c></row></sheetData></worksheet>`)
	got, err := inspect(file, Params{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{nil, {"123", "", "456"}}
	if !reflect.DeepEqual(got.Rows, want) || got.TotalRows != 2 || got.Truncated {
		t.Fatalf("unexpected preview %+v", got)
	}
}

func previewWithWorksheetXML(t *testing.T, worksheetXML string) string {
	t.Helper()
	workbook := excelize.NewFile()
	defer workbook.Close()
	data, err := workbook.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data.Bytes()), int64(data.Len()))
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "preview.xlsx")
	out, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	writer := zip.NewWriter(out)
	for _, entry := range reader.File {
		if entry.Name != "xl/worksheets/sheet1.xml" {
			if err := writer.Copy(entry); err != nil {
				t.Fatal(err)
			}
			continue
		}
		sheet, err := writer.Create(entry.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(sheet, worksheetXML); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return file
}
