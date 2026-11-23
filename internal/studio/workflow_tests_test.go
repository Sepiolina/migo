package studio

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func copyOrdersTests(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "tests"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"orders-totals.yaml", "orders.xlsx", "parts.xlsx", "tests/orders-totals.test.yaml", "tests/orders-totals.xlsx"} {
		source, err := os.ReadFile(filepath.Join(fixtureRoot(t), "examples", path))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), source, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestWorkflowTestsRunSavedRevision(t *testing.T) {
	root := copyOrdersTests(t)
	p := Params{Workspace: root, WorkflowPath: "orders-totals.yaml", YAML: "invalid editor contents"}
	source, err := os.ReadFile(filepath.Join(root, p.WorkflowPath))
	if err != nil {
		t.Fatal(err)
	}
	discovery := request(t, "discoverTests", p)
	if !discovery.OK {
		t.Fatal(discovery.Error)
	}
	found := discovery.Result.(workflowTestsResult)
	if found.WorkflowPath != p.WorkflowPath || found.Revision != fmt.Sprintf("%x", sha256.Sum256(source)) || len(found.Suites) != 1 {
		t.Fatalf("discovery = %+v", found)
	}
	suite := found.Suites[0]
	if suite.Path != "tests/orders-totals.test.yaml" || suite.Name != "orders-totals" || len(suite.Cases) != 2 ||
		suite.Cases[0].Outcome != "" || suite.Cases[1].ExpectedError != "Orders" {
		t.Fatalf("suite = %+v", suite)
	}
	p.Path, p.Revision = suite.Path, found.Revision
	run := request(t, "runTests", p)
	if !run.OK {
		t.Fatal(run.Error)
	}
	cases := run.Result.(workflowTestsResult).Suites[0].Cases
	if cases[0].Outcome != "passed" || cases[0].DurationMS == nil || cases[1].Outcome != "passed" ||
		cases[1].DurationMS == nil || !strings.Contains(cases[1].ActualError, "Orders") {
		t.Fatalf("outcomes = %+v", cases)
	}
	if err := os.WriteFile(filepath.Join(root, p.WorkflowPath), append(source, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if run := request(t, "runTests", p); run.OK || !strings.Contains(run.Error.Message, "saved workflow changed") {
		t.Fatalf("stale revision response = %+v", run)
	}
}

func TestWorkflowTestsMismatchDoesNotWriteGolden(t *testing.T) {
	root := copyOrdersTests(t)
	golden := filepath.Join(root, "tests", "orders-totals.xlsx")
	book, err := excelize.OpenFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	sheet := book.GetSheetList()[0]
	if err := book.SetCellStr(sheet, "A2", "a deliberate difference"); err != nil {
		t.Fatal(err)
	}
	if err := book.Save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	p := Params{Workspace: root, WorkflowPath: "orders-totals.yaml"}
	run := request(t, "runTests", p)
	if !run.OK {
		t.Fatal(run.Error)
	}
	c := run.Result.(workflowTestsResult).Suites[0].Cases[0]
	if c.Outcome != "failed" || c.Mismatch == nil || c.Mismatch.Sheet != sheet || c.Mismatch.Cell != "A2" ||
		c.Mismatch.Expected != "a deliberate difference" || !strings.Contains(c.Error, "cell A2") {
		t.Fatalf("mismatch = %+v (%+v)", c, c.Mismatch)
	}
	after, err := os.ReadFile(golden)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("golden workbook changed, read error: %v", err)
	}

	if err := os.Remove(golden); err != nil {
		t.Fatal(err)
	}
	run = request(t, "runTests", p)
	if !run.OK {
		t.Fatal(run.Error)
	}
	c = run.Result.(workflowTestsResult).Suites[0].Cases[0]
	if c.Outcome != "failed" || !strings.Contains(c.Error, "does not exist") {
		t.Fatalf("missing fixture = %+v", c)
	}
	if _, err := os.Stat(golden); !os.IsNotExist(err) {
		t.Fatalf("missing fixture was recreated: %v", err)
	}
}

func TestWorkflowTestsWorkspaceAndCancellation(t *testing.T) {
	root := copyOrdersTests(t)
	outside := t.TempDir()
	p := Params{Workspace: root, WorkflowPath: "orders-totals.yaml"}
	external := filepath.Join(outside, "escape.test.yaml")
	if err := os.WriteFile(external, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	escaped := p
	escaped.Path = external
	if run := request(t, "runTests", escaped); run.OK || !strings.Contains(strings.ToLower(run.Error.Message), "outside") {
		t.Fatalf("external suite response = %+v", run)
	}
	escaped = p
	escaped.WorkflowPath = external
	if run := request(t, "runTests", escaped); run.OK || !strings.Contains(strings.ToLower(run.Error.Message), "outside") {
		t.Fatalf("external workflow response = %+v", run)
	}
	if err := os.WriteFile(filepath.Join(root, "other.yaml"), []byte("name: another workflow"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "other.test.yaml"), []byte("workflow: other.yaml\ntests: [{name: other, error: expected}]"), 0600); err != nil {
		t.Fatal(err)
	}
	escaped = p
	escaped.Path = "other.test.yaml"
	if run := request(t, "runTests", escaped); run.OK || !strings.Contains(run.Error.Message, "another workflow") {
		t.Fatalf("incorrect suite response = %+v", run)
	}
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	response := Handle(expired, Request{Version: Version, Command: "runTests", Params: p})
	if response.OK || !strings.Contains(response.Error.Message, context.DeadlineExceeded.Error()) {
		t.Fatalf("expired request response = %+v", response)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	response = Handle(canceled, Request{Version: Version, Command: "discoverTests", Params: p})
	if response.OK || !strings.Contains(response.Error.Message, context.Canceled.Error()) {
		t.Fatalf("canceled request response = %+v", response)
	}
}

func TestWorkflowTestsRejectOversizedResponse(t *testing.T) {
	root := copyOrdersTests(t)
	suite := "workflow: orders-totals.yaml\ntests:\n"
	var entries strings.Builder
	entries.WriteString(suite)
	for i := 0; i < 250; i++ {
		fmt.Fprintf(&entries, "  - name: case %d\n    error: '%s'\n", i, strings.Repeat("<", 750))
	}
	if err := os.WriteFile(filepath.Join(root, "large.test.yaml"), []byte(entries.String()), 0600); err != nil {
		t.Fatal(err)
	}
	discovery := request(t, "discoverTests", Params{Workspace: root, WorkflowPath: "orders-totals.yaml"})
	if discovery.OK || discovery.Error.Code != "response_too_large" {
		t.Fatalf("large response = %+v", discovery)
	}
}

func TestWorkflowTestsReportsMalformedSuiteAlongsideResults(t *testing.T) {
	root := copyOrdersTests(t)
	badPath := "tests/broken.test.yaml"
	if err := os.WriteFile(filepath.Join(root, badPath), []byte("workflow: ../orders-totals.yaml\ntests: ["), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"discoverTests", "runTests"} {
		response := request(t, command, Params{Workspace: root, WorkflowPath: "orders-totals.yaml"})
		if !response.OK {
			t.Fatal(response.Error)
		}
		result := response.Result.(workflowTestsResult)
		if len(result.Suites) != 1 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Path != badPath ||
			!strings.Contains(result.Diagnostics[0].Message, "yaml") {
			t.Fatalf("%s = %+v", command, result)
		}
	}
}
