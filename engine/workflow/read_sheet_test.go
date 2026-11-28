package workflow

import "testing"

func TestPickSheet(t *testing.T) {
	names := []string{"Notes", " shortage ", "Data"}
	for _, c := range []struct {
		refs []sheetRef
		want string
		err  string
	}{
		{nil, "Notes", ""},
		{[]sheetRef{{index: 2}}, "Data", ""},
		{[]sheetRef{{name: "Shortage"}}, " shortage ", ""},
		{[]sheetRef{{name: "Priority"}, {index: 1}}, " shortage ", ""},
		{[]sheetRef{{index: 5}}, "", "workbook has 3 sheet(s), no sheet 6"},
		{[]sheetRef{{name: "X"}}, "", `workbook has no sheet named "X"`},
		{[]sheetRef{{name: "X"}, {index: 5}}, "", `workbook has none of the sheets "X", 6 (it has 3: Notes,  shortage , Data)`},
	} {
		got, err := pickSheet(names, c.refs)
		if got != c.want || (err == nil) != (c.err == "") || err != nil && err.Error() != c.err {
			t.Errorf("%v: %q %v", c.refs, got, err)
		}
	}
}
