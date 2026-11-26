package sheet

import (
	"os"
	"reflect"
	"testing"
)

// testdata files were written by SheetJS from a sheet whose range is C3:G5.
var offsetWant = [][]string{
	{"hello", "0.30000000000000004", "", "", ""},
	{"1", "", "", "", ""},
	{"", "true", "1e+21", "1.5e-7", "  pad  "},
}

func TestRangeOffsetAndNumberFormatting(t *testing.T) {
	for _, f := range []string{"../../testdata/offset_range.xlsx", "../../testdata/offset_range.xls"} {
		wb, err := Open(f)
		if err != nil {
			t.Fatal(f, err)
		}
		rows, err := wb.Rows("S1")
		wb.Close()
		if err != nil || !reflect.DeepEqual(rows, offsetWant) {
			t.Errorf("%s: %v %v", f, rows, err)
		}
	}
}

func TestUnsupported(t *testing.T) {
	// Plain text opens as CSV (csv_test.go); binary files that are not
	// workbooks are still rejected.
	path := t.TempDir() + "/x.bin"
	if err := os.WriteFile(path, []byte("\x00\x01binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err != ErrUnsupported {
		t.Error("binary non-workbook should be rejected")
	}
}
