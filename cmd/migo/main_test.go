package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestExampleWorkflow(t *testing.T) {
	out := filepath.Join(t.TempDir(), "summary.xlsx")
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"-in", "parts=../../examples/parts.xlsx", "-check", "../../examples/parts-summary.yaml"}, &stdout, &stderr); code != 0 {
		t.Fatalf("check code %d: %s", code, &stderr)
	}
	if !strings.Contains(stdout.String(), "no output kept") {
		t.Fatalf("stdout=%s", &stdout)
	}
	// Flags may also follow the workflow path.
	if code := run(context.Background(), []string{"../../examples/parts-summary.yaml", "-in", "parts=../../examples/parts.xlsx", "-out", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("run code %d: %s", code, &stderr)
	}
	f, err := excelize.OpenFile(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, _ := f.GetRows("Parts")
	if len(rows) < 2 || !reflect.DeepEqual(rows[0], []string{"Part Number", "Description", "Quantity"}) || !reflect.DeepEqual(rows[1], []string{"00123", "Resistor", "5"}) {
		t.Fatalf("rows=%v", rows)
	}
}

func TestBOMWorkflow(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bom.xlsx")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"-in", "bom=../../testdata/bom_basic.xlsx", "-in", "priority=../../testdata/prio_basic.xlsx", "-out", out, "../../workflows/bom.yaml"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code %d: %s", code, &stderr)
	}
	f, err := excelize.OpenFile(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if got := f.GetSheetList(); !reflect.DeepEqual(got, []string{"BOM", "debug"}) {
		t.Fatalf("sheets=%v", got)
	}
}

func TestUsageErrors(t *testing.T) {
	wf := "../../examples/parts-summary.yaml"
	for _, args := range [][]string{
		{},
		{"-check", wf, "extra.yaml"},
		{"-in", "parts=../../examples/parts.xlsx", wf}, // neither -out nor -check
		{"-check", wf},                                    // missing input
		{"-in", "nope", "-check", wf},                     // bad -in
		{"-in", "parts=x", "-in", "more=y", "-check", wf}, // unknown input
		{"-in", "parts=../../examples/parts.xlsx", "-out", "../../examples/parts.xlsx", wf}, // overwrite input
		{"-cfg", "bad-json", "-check", wf},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(context.Background(), args, &stdout, &stderr); code != 2 || stderr.Len() == 0 {
				t.Fatalf("code=%d stderr=%s", code, &stderr)
			}
		})
	}
	if _, err := os.Stat("../../examples/parts.xlsx"); err != nil {
		t.Fatal("input damaged")
	}
}
