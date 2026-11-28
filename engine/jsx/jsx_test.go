package jsx

import (
	"math"
	"testing"
)

func TestNumToString(t *testing.T) {
	a, b := 0.1, 0.2 // variables: constant folding would make 0.1+0.2 exactly 0.3
	cases := map[float64]string{
		0: "0", 1: "1", a + b: "0.30000000000000004", 1e21: "1e+21", 1e20: "100000000000000000000",
		1.5e-7: "1.5e-7", 0.000001: "0.000001", -2.5: "-2.5", 1234567.5: "1234567.5", math.Copysign(0, -1): "0",
	}
	for in, want := range cases {
		if got := NumToString(in); got != want {
			t.Errorf("NumToString(%v)=%q want %q", in, got, want)
		}
	}
}

func TestParseFloat(t *testing.T) {
	ok := map[string]float64{"12abc": 12, " 3.5 ": 3.5, ".5": 0.5, "-1e3x": -1000, "5.": 5, "1e": 1, "+7": 7}
	for in, want := range ok {
		if got, o := ParseFloat(in); !o || got != want {
			t.Errorf("ParseFloat(%q)=%v,%v want %v", in, got, o, want)
		}
	}
	for _, in := range []string{"", "abc", ".", "-", "e5"} {
		if _, o := ParseFloat(in); o {
			t.Errorf("ParseFloat(%q) should be NaN", in)
		}
	}
	if v, o := ParseFloat("-Infinity"); !o || !math.IsInf(v, -1) {
		t.Error("Infinity")
	}
}

func TestTrimNormToFloat(t *testing.T) {
	if Trim("\uFEFF \u00A0x\t\n") != "x" {
		t.Error("trim must remove BOM and NBSP like JS")
	}
	if Norm(" abc ") != "ABC" {
		t.Error("norm")
	}
	if ToFloat("1,234.5", 0) != 1234.5 || ToFloat("x", 7) != 7 {
		t.Error("tofloat")
	}
}

func TestTruthyAndString(t *testing.T) {
	for v, want := range map[any]bool{nil: false, "": false, "0": true, 0.0: false, 2.0: true, false: false, true: true, math.NaN(): false} {
		if Truthy(v) != want {
			t.Errorf("Truthy(%v) want %v", v, want)
		}
	}
	if String(nil) != "null" || String(1.0) != "1" || String(true) != "true" || String(map[string]any{}) != "[object Object]" {
		t.Error("String")
	}
	b, _ := Stringify(map[string]string{"a": "<&>"})
	if string(b) != `{"a":"<&>"}` {
		t.Errorf("Stringify %s", b)
	}
}
