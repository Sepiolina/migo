package workflow

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func strptr(v string) *string                         { return &v }
func numptr(v float64) *float64                       { return &v }
func boolptr(v bool) *bool                            { return &v }
func table(columns []string, rows ...[]string) *Table { return &Table{Columns: columns, Rows: rows} }

func apply(t *testing.T, input *Table, step Step, datasets map[string]*Table) *Table {
	t.Helper()
	before := CloneTable(input)
	other := make(map[string]*Table, len(datasets))
	for k, v := range datasets {
		other[k] = CloneTable(v)
	}
	out, err := ApplyStep(input, step, datasets)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, before) {
		t.Fatalf("input mutated: %#v -> %#v", before, input)
	}
	for k, v := range datasets {
		if !reflect.DeepEqual(v, other[k]) {
			t.Fatalf("dataset %q mutated", k)
		}
	}
	if err := ValidateTable(out); err != nil {
		t.Fatal(err)
	}
	return out
}
func wantTable(t *testing.T, got, want *Table) {
	t.Helper()
	if !reflect.DeepEqual(got.Columns, want.Columns) || !reflect.DeepEqual(got.Rows, want.Rows) {
		t.Fatalf("got columns %v rows %v; want columns %v rows %v", got.Columns, got.Rows, want.Columns, want.Rows)
	}
}

func TestOperationsMap(t *testing.T) {
	input := table([]string{"ID", "A", "B"}, []string{"001", "a", "b"})
	cases := []struct {
		name string
		spec MapSpec
		want *Table
	}{
		{"projection", MapSpec{Columns: []Mapping{{From: "@A", To: "Part"}, {From: "B", To: "Other"}}}, table([]string{"Part", "Other"}, []string{"001", "b"})},
		{"copy", MapSpec{Keep: true, Columns: []Mapping{{From: "ID", To: "Copy"}}}, table([]string{"ID", "A", "B", "Copy"}, []string{"001", "a", "b", "001"})},
		{"move", MapSpec{Keep: true, Columns: []Mapping{{From: "A", To: "Moved", Move: true}}}, table([]string{"ID", "B", "Moved"}, []string{"001", "b", "a"})},
		{"identity", MapSpec{Keep: true, Columns: []Mapping{{From: "A", To: "A", Move: true}}}, input},
		{"swap", MapSpec{Keep: true, Columns: []Mapping{{From: "A", To: "B", Move: true}, {From: "B", To: "A", Move: true}}}, table([]string{"ID", "B", "A"}, []string{"001", "a", "b"})},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantTable(t, apply(t, input, Step{Map: &c.spec}, nil), c.want) })
	}
}

func TestOperationsFilter(t *testing.T) {
	input := table([]string{"Kind", "Qty"}, []string{"keep", "2"}, []string{"other", "4"}, []string{"skip", "3"}, []string{"keep", "1"})
	f := FilterSpec{All: []FilterSpec{{Column: "Qty", GreaterOrEqual: numptr(2), LessThan: numptr(4)}}, Any: []FilterSpec{{Column: "Kind", Equal: strptr("keep")}, {Column: "Kind", Contains: strptr("kip")}}, Column: "Kind", NotEqual: strptr("skip"), In: []string{"keep", "skip"}, Blank: boolptr(false)}
	wantTable(t, apply(t, input, Step{Filter: &f}, nil), table(input.Columns, []string{"keep", "2"}))
	blank := table([]string{"A"}, []string{" "}, []string{"x"})
	wantTable(t, apply(t, blank, Step{Filter: &FilterSpec{Column: "A", Blank: boolptr(true)}}, nil), table(blank.Columns, []string{" "}))
	emptyIn := FilterSpec{Column: "Kind", In: []string{}}
	if got := apply(t, input, Step{Filter: &emptyIn}, nil); len(got.Rows) != 0 {
		t.Fatal(got.Rows)
	}
}

func TestOperationsFillAndDedupe(t *testing.T) {
	input := table([]string{"A", "B"}, []string{" ", "x"}, []string{"a", ""}, []string{"\t", "y"}, []string{"a", "y"})
	filled := apply(t, input, Step{ForwardFill: &ColumnsSpec{Columns: []string{"A"}}}, nil)
	wantTable(t, filled, table(input.Columns, []string{"", "x"}, []string{"a", ""}, []string{"a", "y"}, []string{"a", "y"}))
	wantTable(t, apply(t, filled, Step{Deduplicate: &ColumnsSpec{Columns: []string{"A", "B"}}}, nil), table(input.Columns, []string{"", "x"}, []string{"a", ""}, []string{"a", "y"}))
	wantTable(t, apply(t, filled, Step{Deduplicate: &ColumnsSpec{Columns: []string{"A"}}}, nil), table(input.Columns, []string{"", "x"}, []string{"a", ""}))
}

func TestOperationsAggregate(t *testing.T) {
	input := table([]string{"Group", "N", "Text"}, []string{"b", "2", "x"}, []string{"a", "10", "z"}, []string{"b", "4", "x"}, []string{"b", "6", " "}, []string{"b", "8", "y"})
	grouped := apply(t, input, Step{Group: &GroupSpec{By: []string{"Group"}, Aggregate: map[string]string{"Text": "join", "N": "sum"}}}, nil)
	wantTable(t, grouped, table(input.Columns, []string{"b", "20", "x, y"}, []string{"a", "10", "z"}))
	cases := map[string]string{"sum": "30", "first": "2", "last": "8", "min": "2", "max": "10", "count": "5", "mean": "6"}
	for op, want := range cases {
		t.Run(op, func(t *testing.T) {
			got := apply(t, input, Step{Aggregate: &AggregateSpec{Columns: map[string]string{"N": op}}}, nil)
			wantTable(t, got, table([]string{"N"}, []string{want}))
		})
	}
	for _, op := range []string{"sum", "count", "first", "last", "min", "max", "mean", "join"} {
		t.Run("empty_"+op, func(t *testing.T) {
			want := ""
			if op == "sum" || op == "count" {
				want = "0"
			}
			got := apply(t, table([]string{"N"}), Step{Aggregate: &AggregateSpec{Columns: map[string]string{"N": op}}}, nil)
			wantTable(t, got, table([]string{"N"}, []string{want}))
		})
	}
	if got := apply(t, table(input.Columns), Step{Group: &GroupSpec{By: []string{"Group"}, Aggregate: map[string]string{"N": "sum"}}}, nil); len(got.Rows) != 0 {
		t.Fatal(got.Rows)
	}
}

func TestOperationsSort(t *testing.T) {
	input := table([]string{"N", "Tag"}, []string{"2", "first"}, []string{"10", "ten"}, []string{"2", "second"})
	wantTable(t, apply(t, input, Step{Sort: &SortSpec{By: []string{"N"}, Numeric: true}}, nil), table(input.Columns, []string{"2", "first"}, []string{"2", "second"}, []string{"10", "ten"}))
	wantTable(t, apply(t, input, Step{Sort: &SortSpec{By: []string{"N"}, Numeric: true, Order: "descending"}}, nil), table(input.Columns, []string{"10", "ten"}, []string{"2", "first"}, []string{"2", "second"}))
	wantTable(t, apply(t, input, Step{Sort: &SortSpec{By: []string{"N"}}}, nil), table(input.Columns, []string{"10", "ten"}, []string{"2", "first"}, []string{"2", "second"}))
}

func TestOperationsCalculate(t *testing.T) {
	input := table([]string{"A", "B"}, []string{"12", "2"})
	for op, want := range map[string]string{"add": "16", "subtract": "8", "multiply": "48", "divide": "3"} {
		t.Run(op, func(t *testing.T) {
			got := apply(t, input, Step{Calculate: &CalculateSpec{Column: "Result", Operation: op, Columns: []string{"A", "B"}, Value: numptr(2)}}, nil)
			wantTable(t, got, table([]string{"A", "B", "Result"}, []string{"12", "2", want}))
		})
	}
	got := apply(t, input, Step{Calculate: &CalculateSpec{Column: "A", Operation: "add", Columns: []string{"A", "B"}}}, nil)
	wantTable(t, got, table(input.Columns, []string{"14", "2"}))
	got = apply(t, input, Step{Calculate: &CalculateSpec{Column: "Constant", Operation: "add", Value: numptr(3)}}, nil)
	wantTable(t, got, table([]string{"A", "B", "Constant"}, []string{"12", "2", "3"}))
}

func TestOperationsTransform(t *testing.T) {
	for _, c := range []struct{ op, in, want string }{{"trim", " a ", "a"}, {"upper", "aB", "AB"}, {"lower", "aB", "ab"}, {"normalize", " ab ", "AB"}, {"replace", "a-a", "x-x"}} {
		t.Run(c.op, func(t *testing.T) {
			got := apply(t, table([]string{"A"}, []string{c.in}), Step{Transform: &TransformSpec{Columns: []string{"A"}, Operation: c.op, Old: "a", New: "x"}}, nil)
			wantTable(t, got, table([]string{"A"}, []string{c.want}))
		})
	}
}

func TestOperationsStyleAndClone(t *testing.T) {
	input := table([]string{"ID", "Amount"}, []string{"001", "2"})
	style := &StyleSpec{Header: &HeaderStyle{Bold: true}, NumberFormats: map[string]string{"@B": "0.00"}}
	got := apply(t, input, Step{Style: style}, nil)
	style.Header.Bold = false
	style.NumberFormats["@B"] = "changed"
	if !got.Style.Header.Bold || got.Style.NumberFormats["Amount"] != "0.00" {
		t.Fatal(got.Style)
	}
	renamed := apply(t, got, Step{Map: &MapSpec{Columns: []Mapping{{From: "Amount", To: "Total"}}}}, nil)
	if !reflect.DeepEqual(renamed.Style.NumberFormats, map[string]string{"Total": "0.00"}) {
		t.Fatal(renamed.Style)
	}
	clone := CloneTable(got)
	clone.Rows[0][0] = "x"
	clone.Columns[0] = "x"
	clone.Style.Header.Bold = false
	clone.Style.NumberFormats["Amount"] = "x"
	if got.Rows[0][0] != "001" || got.Columns[0] != "ID" || !got.Style.Header.Bold || got.Style.NumberFormats["Amount"] != "0.00" {
		t.Fatal("clone shares data")
	}
	disabled := apply(t, input, Step{Enabled: boolptr(false), Transform: &TransformSpec{Operation: "invalid"}}, nil)
	wantTable(t, disabled, input)
	disabled.Rows[0][0] = "changed"
	if input.Rows[0][0] != "001" {
		t.Fatal("disabled result aliases input")
	}
	if CloneTable(nil) != nil {
		t.Fatal("nil clone")
	}
}

func TestOperationsValidationErrors(t *testing.T) {
	for _, input := range []*Table{nil, table([]string{" "}), table([]string{"A", "A"}), table([]string{"A"}, []string{})} {
		if ValidateTable(input) == nil {
			t.Fatalf("accepted %#v", input)
		}
	}
	input := table([]string{"A", "B"}, []string{"1", "2"})
	cases := []Step{
		{}, {Sort: &SortSpec{}, Map: &MapSpec{}},
		{Map: &MapSpec{Keep: true, Columns: []Mapping{{From: "A", To: "B"}}}},
		{Map: &MapSpec{Columns: []Mapping{{From: "A", To: "X"}, {From: "B", To: "X"}}}},
		{Map: &MapSpec{Columns: []Mapping{{From: "A", To: " "}}}},
		{Map: &MapSpec{Columns: []Mapping{{From: "@ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ", To: "X"}}}},
		{Filter: &FilterSpec{}}, {Filter: &FilterSpec{Column: "missing", Equal: strptr("x")}},
		{Filter: &FilterSpec{Column: "A", GreaterThan: numptr(math.Inf(1))}},
		{Group: &GroupSpec{By: []string{"A"}, Aggregate: map[string]string{"A": "sum"}}},
		{Aggregate: &AggregateSpec{Columns: map[string]string{"B": "unknown"}}},
		{Aggregate: &AggregateSpec{Columns: map[string]string{"B": "sum", "@B": "count"}}},
		{Sort: &SortSpec{By: []string{"A"}, Order: "invalid"}},
		{Sort: &SortSpec{By: []string{"A", "@A"}}},
		{ForwardFill: &ColumnsSpec{Columns: []string{"@a"}}},
		{Calculate: &CalculateSpec{Column: "C", Operation: "invalid", Columns: []string{"A"}}},
		{Calculate: &CalculateSpec{Column: "C", Operation: "add"}},
		{Calculate: &CalculateSpec{Column: " ", Operation: "add", Value: numptr(1)}},
		{Calculate: &CalculateSpec{Column: "C", Operation: "divide", Columns: []string{"A"}, Value: numptr(0)}},
		{Transform: &TransformSpec{Columns: []string{"A"}, Operation: "invalid"}},
		{Style: &StyleSpec{NumberFormats: map[string]string{"missing": "0"}}},
		{Style: &StyleSpec{NumberFormats: map[string]string{"A": "0", "@A": "0.0"}}},
	}
	for i, step := range cases {
		before := CloneTable(input)
		if _, err := ApplyStep(input, step, nil); err == nil {
			t.Errorf("case %d accepted", i)
		}
		if !reflect.DeepEqual(input, before) {
			t.Errorf("case %d mutated input", i)
		}
	}
	for _, v := range []string{"", "NaN", "+Inf", "invalid", "1e999"} {
		bad := table([]string{"A"}, []string{v})
		steps := []Step{{Filter: &FilterSpec{Column: "A", GreaterThan: numptr(0)}}, {Sort: &SortSpec{By: []string{"A"}, Numeric: true}}, {Aggregate: &AggregateSpec{Columns: map[string]string{"A": "sum"}}}, {Calculate: &CalculateSpec{Column: "B", Operation: "add", Columns: []string{"A"}}}}
		for _, step := range steps {
			if _, err := ApplyStep(bad, step, nil); err == nil || !strings.Contains(err.Error(), "row 1 column \"A\"") {
				t.Errorf("value %q error lacks context: %v", v, err)
			}
		}
	}
	for _, v := range []float64{math.NaN(), math.Inf(1)} {
		if _, err := ApplyStep(table([]string{"A"}), Step{Calculate: &CalculateSpec{Column: "X", Operation: "add", Value: &v}}, nil); err == nil {
			t.Fatal("empty table accepted nonfinite constant")
		}
	}
	overflow := table([]string{"A"}, []string{"1e308"}, []string{"1e308"})
	if _, err := ApplyStep(overflow, Step{Aggregate: &AggregateSpec{Columns: map[string]string{"A": "sum"}}}, nil); err == nil || !strings.Contains(err.Error(), "column \"A\"") {
		t.Fatal(err)
	}
}
