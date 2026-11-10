package sheet

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOpenCSV(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name, body string
		want       [][]string
	}{
		{"comma", "\xEF\xBB\xBFID,Name,Qty\n1,\"Acme, Inc\",3\n2,Beta\n", [][]string{{"ID", "Name", "Qty"}, {"1", "Acme, Inc", "3"}, {"2", "Beta", ""}}},
		{"semicolon", "ID;Price\r\n1;4,50\r\n", [][]string{{"ID", "Price"}, {"1", "4,50"}}},
		{"tab", "A\tB\nx\ty\n", [][]string{{"A", "B"}, {"x", "y"}}},
		{"cp1252", "Name\nCaf\xe9 \x80\n", [][]string{{"Name"}, {"Café €"}}},
	}
	for _, c := range cases {
		path := filepath.Join(dir, c.name)
		if err := os.WriteFile(path, []byte(c.body), 0o600); err != nil {
			t.Fatal(err)
		}
		wb, err := Open(path)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := wb.SheetNames(); !reflect.DeepEqual(got, []string{CSVSheet}) {
			t.Errorf("%s: sheets %v", c.name, got)
		}
		rows, err := wb.Rows(CSVSheet)
		if err != nil || !reflect.DeepEqual(rows, c.want) {
			t.Errorf("%s: %q, %v", c.name, rows, err)
		}
		if _, err := wb.Rows("Other"); err == nil {
			t.Errorf("%s: unknown sheet accepted", c.name)
		}
	}
	bin := filepath.Join(dir, "bin")
	os.WriteFile(bin, []byte{1, 0, 2, 0}, 0o600)
	if _, err := Open(bin); err != ErrUnsupported {
		t.Errorf("binary file: %v", err)
	}
}
