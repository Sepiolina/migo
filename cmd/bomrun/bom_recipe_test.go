package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"
	"gopkg.in/yaml.v3"
	"miservice/internal/modules/bom"
)

func TestBOMRecipeCLIParity(t *testing.T) {
	dir := t.TempDir()
	recipe, err := bom.DefaultRecipe()
	if err != nil {
		t.Fatal(err)
	}
	recipe.Input["bom"], err = filepath.Abs("../../testdata/bom_basic.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	recipe.Input["priority"], err = filepath.Abs("../../testdata/prio_basic.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	recipe.Output.File = filepath.Join(dir, "recipe.xlsx")
	body, err := yaml.Marshal(recipe)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "recipe.yaml")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(dir, "stub.json")
	if err := os.WriteFile(stub, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	args := []string{"-workflow", path, "-cfg", `{"db_enabled":true,"sap_enabled":false}`, "-stubdb", stub}
	if code := run(context.Background(), append(append([]string{}, args...), "-check"), &stdout, &stderr); code != 0 {
		t.Fatalf("check %d: %s", code, &stderr)
	}
	if _, err := os.Stat(recipe.Output.File); !os.IsNotExist(err) {
		t.Fatalf("check published output: %v", err)
	}
	if code := run(context.Background(), args, &stdout, &stderr); code != 0 {
		t.Fatalf("run %d: %s", code, &stderr)
	}
	reference := filepath.Join(dir, "legacy.xlsx")
	if code := run(context.Background(), []string{"-bom", recipe.Input["bom"], "-prio", recipe.Input["priority"], "-out", reference, "-cfg", `{"db_enabled":true,"sap_enabled":false}`, "-stubdb", stub}, &stdout, &stderr); code != 0 {
		t.Fatalf("legacy %d: %s", code, &stderr)
	}
	actual, err := excelize.OpenFile(recipe.Output.File)
	if err != nil {
		t.Fatal(err)
	}
	defer actual.Close()
	expected, err := excelize.OpenFile(reference)
	if err != nil {
		t.Fatal(err)
	}
	defer expected.Close()
	if !reflect.DeepEqual(actual.GetSheetList(), []string{"BOM", "debug"}) {
		t.Fatal(actual.GetSheetList())
	}
	for _, name := range expected.GetSheetList() {
		got, err := actual.GetRows(name)
		if err != nil {
			t.Fatal(err)
		}
		want, err := expected.GetRows(name)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("CLI recipe differs in %s", name)
		}
	}
	for _, extra := range [][]string{{"-bom", "ignored"}, {"-prio", "ignored"}, {"-out", "ignored"}} {
		if code := run(context.Background(), append(append([]string{}, args...), extra...), &stdout, &stderr); code != 2 {
			t.Fatalf("conflicting file paths accepted: %v", extra)
		}
	}
}
