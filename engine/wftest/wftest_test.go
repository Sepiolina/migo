package wftest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspacePaths(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	writeFixture(t, filepath.Join(outside, "private.json"), "[]")
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(outside, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	for _, path := range []string{filepath.Join(outside, "private.json"), "alias/private.json", "alias/missing.json"} {
		if _, err := WorkspacePath(root, root, path); err == nil || !strings.Contains(err.Error(), "outside the workspace") {
			t.Errorf("WorkspacePath(%q) = %v", path, err)
		}
	}
	if got, err := WorkspacePath(root, root, "missing/nested.json"); err != nil || got != filepath.Join(root, "missing", "nested.json") {
		t.Fatalf("missing in-workspace path = %q, %v", got, err)
	}

	writeFixture(t, filepath.Join(root, "fixtures", "suite"), "workflow: workflow.yaml\ntests: [{name: expected, error: something}]")
	if err := os.Symlink(filepath.Join(root, "fixtures", "suite"), filepath.Join(root, "suite.test.yaml")); err != nil {
		t.Fatal(err)
	}
	file, err := LoadWorkspace(root, "suite.test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := file.WorkflowPath(root)
	if err != nil || workflow != filepath.Join(root, "workflow.yaml") {
		t.Fatalf("workflow from a linked suite = %q, %v", workflow, err)
	}
}

func TestWorkspaceSuiteSize(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "large.test.yaml")
	writeFixture(t, path, "workflow: workflow.yaml\ntests: [{name: example, error: example}]\n#"+strings.Repeat(" ", 1<<20))
	if _, err := LoadWorkspace(root, path); err == nil || !strings.Contains(err.Error(), "at most 1 MiB") {
		t.Fatalf("oversized workspace suite error = %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("CLI suite load error = %v", err)
	}
}

func TestWorkspaceRun(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "input.csv"), "ID\n1\n")
	writeFixture(t, filepath.Join(root, "workflow.yaml"), `name: REST fixture
input: {input: {label: Input}}
steps:
  - from: input
  - rest:
      url: http://example.invalid
      save: data
  - from: data
  - save: result
output:
  format: csv
  sheets: {Result: result}
`)
	writeFixture(t, filepath.Join(root, "tests", "reply.json"), `[{"ID":2}]`)
	golden := filepath.Join(root, "tests", "expected.csv")
	writeFixture(t, golden, "\ufeffID\n3\n")
	writeFixture(t, filepath.Join(root, "tests", "suite.test.yaml"), `workflow: ../workflow.yaml
tests:
  - name: offline stub
    in: {input: ../input.csv}
    rest_stub: reply.json
    expect: expected.csv
  - name: offline error
    in: {input: ../input.csv}
    error: rest steps have no network in tests
  - name: missing fixture
    in: {input: ../input.csv}
    rest_stub: reply.json
    expect: missing.csv
  - name: outside stub
    in: {input: ../input.csv}
    rest_stub: ../../reply.json
    error: outside
  - name: outside SQL stub
    in: {input: ../input.csv}
    sql_stub: ../../reply.json
    error: outside
  - name: outside golden
    in: {input: ../input.csv}
    rest_stub: reply.json
    expect: ../../expected.csv
  - name: outside input
    in: {input: ../../input.csv}
    error: outside
`)
	suite, err := LoadWorkspace(root, "tests/suite.test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	results := suite.RunWorkspace(context.Background(), root)
	mismatch := results[0]
	if mismatch.Err == nil || mismatch.Err.Error() != `line 2: got "2", expected "3"` {
		t.Fatalf("mismatch error = %v", mismatch.Err)
	}
	if d := mismatch.Difference; d == nil || d.Kind != "line" || d.Line != 2 || d.Expected != "3" || d.Actual != "2" {
		t.Fatalf("mismatch details = %+v", d)
	}
	if r := results[1]; r.Err != nil || r.ExpectedError == "" || !strings.Contains(r.ActualError, r.ExpectedError) {
		t.Fatalf("expected-error result = %+v", r)
	}
	if r := results[2]; r.Err == nil || !strings.Contains(r.Err.Error(), "missing.csv does not exist yet") {
		t.Fatalf("missing fixture result = %+v", r)
	}
	for _, r := range results[3:] {
		if r.Err == nil || !strings.Contains(r.Err.Error(), "outside the workspace") {
			t.Errorf("unsafe fixture result = %+v", r)
		}
	}
	got, err := os.ReadFile(golden)
	if err != nil || string(got) != "\ufeffID\n3\n" {
		t.Fatalf("golden after run = %q, %v", got, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, result := range suite.RunWorkspace(ctx, root) {
		if !errors.Is(result.Err, context.Canceled) {
			t.Errorf("canceled result = %+v", result)
		}
	}
}

func TestWorkbookDifference(t *testing.T) {
	root := t.TempDir()
	got, want := filepath.Join(root, "got.xlsx"), filepath.Join(root, "want.xlsx")
	for path, value := range map[string]string{got: "actual", want: "expected"} {
		wb := excelize.NewFile()
		if err := wb.SetCellStr("Sheet1", "B2", value); err != nil {
			t.Fatal(err)
		}
		if err := wb.SaveAs(path); err != nil {
			t.Fatal(err)
		}
		if err := wb.Close(); err != nil {
			t.Fatal(err)
		}
	}
	d, err := compare(context.Background(), got, want)
	if err != nil || d == nil || d.Kind != "cell" || d.Sheet != "Sheet1" || d.Cell != "B2" || d.Actual != "actual" || d.Expected != "expected" {
		t.Fatalf("workbook difference = %+v, %v", d, err)
	}
	if d.Error() != `sheet "Sheet1" cell B2: got "actual", expected "expected"` {
		t.Fatal(d.Error())
	}
}
