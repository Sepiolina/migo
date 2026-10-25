package workflow

import (
	"reflect"
	"strings"
	"testing"
)

func TestOperationsJoin(t *testing.T) {
	left := table([]string{"ID", "Value"}, []string{" a ", "L1"}, []string{"A", "L2"}, []string{"missing", "L3"})
	right := table([]string{"Key", "Value", "Extra"}, []string{"A", "R1", "one"}, []string{"a", "R2", "two"}, []string{"other", "R3", "three"})
	data := map[string]*Table{"right": right}
	spec := JoinSpec{With: "right", On: []string{"ID"}, RightOn: []string{"Key"}, Normalize: true}
	got := apply(t, left, Step{Join: &spec}, data)
	wantTable(t, got, table([]string{"ID", "Value", "Value_right", "Extra"}, []string{" a ", "L1", "R1", "one"}, []string{" a ", "L1", "R2", "two"}, []string{"A", "L2", "R1", "one"}, []string{"A", "L2", "R2", "two"}, []string{"missing", "L3", "", ""}))
	// Joining never aliases right-side cells or any dataset storage.
	got.Rows[0][2] = "changed"
	if right.Rows[0][1] != "R1" {
		t.Fatal("join aliases right dataset")
	}
	spec.How = "inner"
	spec.Suffix = "_baseline"
	inner := apply(t, left, Step{Join: &spec}, data)
	if len(inner.Rows) != 4 || inner.Columns[2] != "Value_baseline" {
		t.Fatal(inner)
	}
	spec.Normalize = false
	exact := apply(t, left, Step{Join: &spec}, data)
	wantTable(t, exact, table([]string{"ID", "Value", "Value_baseline", "Extra"}, []string{"A", "L2", "R1", "one"}))
	spec.How = "left"
	empty := apply(t, left, Step{Join: &spec}, map[string]*Table{"right": table(right.Columns)})
	if len(empty.Rows) != 3 || empty.Rows[0][2] != "" {
		t.Fatal(empty)
	}
	emptyLeft := apply(t, table(left.Columns), Step{Join: &spec}, data)
	if len(emptyLeft.Rows) != 0 || len(emptyLeft.Columns) != 4 {
		t.Fatal(emptyLeft)
	}
	defaultKeys := apply(t, table([]string{"ID"}, []string{"1"}), Step{Join: &JoinSpec{With: "r", On: []string{"ID"}}}, map[string]*Table{"r": table([]string{"ID", "V"}, []string{"1", "v"})})
	wantTable(t, defaultKeys, table([]string{"ID", "V"}, []string{"1", "v"}))
	// JSON tuple keys prevent concatenation or separator collisions.
	composite := apply(t, table([]string{"A", "B"}, []string{"a|b", "c"}, []string{"a", "b|c"}), Step{Join: &JoinSpec{With: "r", On: []string{"A", "B"}}}, map[string]*Table{"r": table([]string{"A", "B", "V"}, []string{"a", "b|c", "hit"})})
	wantTable(t, composite, table([]string{"A", "B", "V"}, []string{"a|b", "c", ""}, []string{"a", "b|c", "hit"}))
}

func TestOperationsMerge(t *testing.T) {
	left := table([]string{"ID", "Value"}, []string{"001", "left"})
	data := map[string]*Table{"reordered": table([]string{"Value", "ID"}, []string{"right", "002"}), "empty": table(left.Columns)}
	got := apply(t, left, Step{Merge: &MergeSpec{With: []string{"empty", "reordered", "reordered"}}}, data)
	wantTable(t, got, table(left.Columns, []string{"001", "left"}, []string{"002", "right"}, []string{"002", "right"}))
	got.Rows[1][0] = "changed"
	if data["reordered"].Rows[0][1] != "002" {
		t.Fatal("merge aliases dataset")
	}
}

func TestOperationsCompare(t *testing.T) {
	current := table([]string{"ID", "Qty", "Current"}, []string{"same", "2", ""}, []string{"change", "4", ""}, []string{"added", "8", "x"})
	baseline := table([]string{"ID", "Qty", "Old"}, []string{"removed", "9", "old"}, []string{"change", "3", ""}, []string{"same", "2", ""})
	spec := CompareSpec{With: "base", By: []string{"ID"}}
	got := apply(t, current, Step{Compare: &spec}, map[string]*Table{"base": baseline})
	wantTable(t, got, table([]string{"ID", "Qty", "Current", "Old", "status"}, []string{"same", "2", "", "", "unchanged"}, []string{"change", "4", "", "", "changed"}, []string{"added", "8", "x", "", "added"}, []string{"removed", "9", "", "old", "removed"}))
	// Optional selected columns control value comparison; normalized keys retain
	// their original cell values in the current-first output.
	selected := table([]string{"ID", "Qty", "Note"}, []string{" a ", "1", "new"})
	old := table(selected.Columns, []string{"A", "1", "old"})
	spec = CompareSpec{With: "base", By: []string{"ID"}, Columns: []string{"Qty"}, Normalize: true, StatusColumn: "Diff"}
	wantTable(t, apply(t, selected, Step{Compare: &spec}, map[string]*Table{"base": old}), table([]string{"ID", "Qty", "Note", "Diff"}, []string{" a ", "1", "new", "unchanged"}))
	// Added or removed schema fields with nonblank cells count as a change.
	schema := apply(t, table([]string{"ID", "New"}, []string{"A", "new"}), Step{Compare: &CompareSpec{With: "base", By: []string{"ID"}}}, map[string]*Table{"base": table([]string{"ID", "Old"}, []string{"A", "old"})})
	wantTable(t, schema, table([]string{"ID", "New", "Old", "status"}, []string{"A", "new", "", "changed"}))
	onlyKeys := table([]string{"ID"}, []string{"A"})
	wantTable(t, apply(t, onlyKeys, Step{Compare: &CompareSpec{With: "base", By: []string{"ID"}}}, map[string]*Table{"base": onlyKeys}), table([]string{"ID", "status"}, []string{"A", "unchanged"}))
	empty := apply(t, table(current.Columns), Step{Compare: &CompareSpec{With: "base", By: []string{"ID"}}}, map[string]*Table{"base": table(baseline.Columns)})
	if len(empty.Rows) != 0 || !reflect.DeepEqual(empty.Columns, []string{"ID", "Qty", "Current", "Old", "status"}) {
		t.Fatal(empty)
	}
}

func TestOperationsDatasetErrors(t *testing.T) {
	left := table([]string{"ID", "Value"}, []string{"A", "1"})
	data := map[string]*Table{
		"right":          table([]string{"ID", "Value"}, []string{"A", "2"}),
		"duplicate":      table([]string{"ID", "Value"}, []string{"A", "2"}, []string{" a ", "3"}),
		"exactduplicate": table([]string{"ID", "Value"}, []string{"A", "2"}, []string{"A", "3"}),
		"bad":            table([]string{"ID"}, []string{"A", "invalid"}),
		"nil":            nil,
		"different":      table([]string{"ID", "Other"}, []string{"A", "2"}),
		"short":          table([]string{"ID"}, []string{"A"}),
		"collision":      table([]string{"ID", "Value", "Value_right"}, []string{"A", "2", "3"}),
	}
	cases := []Step{
		{Join: &JoinSpec{With: "missing", On: []string{"ID"}}},
		{Join: &JoinSpec{With: "bad", On: []string{"ID"}}},
		{Join: &JoinSpec{With: "nil", On: []string{"ID"}}},
		{Join: &JoinSpec{With: "right"}},
		{Join: &JoinSpec{With: "right", On: []string{"ID", "Value"}, RightOn: []string{"ID"}}},
		{Join: &JoinSpec{With: "right", On: []string{"missing"}}},
		{Join: &JoinSpec{With: "right", On: []string{"ID"}, How: "outer"}},
		{Join: &JoinSpec{With: "collision", On: []string{"ID"}}},
		{Merge: &MergeSpec{With: []string{"different"}}},
		{Merge: &MergeSpec{With: []string{"short"}}},
		{Merge: &MergeSpec{With: []string{"missing"}}},
		{Compare: &CompareSpec{With: "right"}},
		{Compare: &CompareSpec{With: "right", By: []string{"unknown"}}},
		{Compare: &CompareSpec{With: "right", By: []string{"ID"}, StatusColumn: "Value"}},
		{Compare: &CompareSpec{With: "right", By: []string{"ID"}, StatusColumn: " "}},
		{Compare: &CompareSpec{With: "different", By: []string{"ID"}, Columns: []string{"Value"}}},
		{Compare: &CompareSpec{With: "right", By: []string{"ID"}, Columns: []string{"Value", "@B"}}},
		{Compare: &CompareSpec{With: "duplicate", By: []string{"ID"}, Normalize: true}},
		{Compare: &CompareSpec{With: "exactduplicate", By: []string{"ID"}}},
	}
	for i, step := range cases {
		before := CloneTable(left)
		if _, err := ApplyStep(left, step, data); err == nil {
			t.Errorf("case %d accepted", i)
		}
		if !reflect.DeepEqual(left, before) {
			t.Errorf("case %d mutated input", i)
		}
	}
	for _, normalize := range []bool{false, true} {
		duplicate := data["exactduplicate"]
		if normalize {
			duplicate = data["duplicate"]
		}
		if _, err := ApplyStep(duplicate, Step{Compare: &CompareSpec{With: "right", By: []string{"ID"}, Normalize: normalize}}, data); err == nil || !strings.Contains(err.Error(), "duplicate compare key in current table") {
			t.Fatal(err)
		}
	}
}
