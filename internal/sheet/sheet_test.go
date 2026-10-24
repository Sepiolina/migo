package sheet

import (
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
	if _, err := Open("sheet.go"); err != ErrUnsupported {
		t.Error("plain text should be rejected")
	}
}
