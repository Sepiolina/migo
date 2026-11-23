package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestListRunsPagesPublishedHistory(t *testing.T) {
	workspace := t.TempDir()
	otherWorkspace := t.TempDir()
	if page := listRunsForTest(t, workspace, 0, 1); len(page.Runs) != 0 || page.Runs == nil || page.NextOffset != 0 {
		t.Fatalf("missing history: %+v", page)
	}

	base := time.Date(2026, 11, 20, 15, 4, 0, 0, time.UTC)
	legacy := makeListedRun(t, workspace, "run-legacy", "output.xlsx", nil, base)
	malformed := makeListedRun(t, workspace, "run-malformed", "output.csv", []byte(`{"version":1,`), base.Add(time.Second))
	latestManifest := listedManifest("run-new", base.Add(2*time.Second))
	latest := makeListedRun(t, workspace, "run-new", "output.csv", encodeListedManifest(t, latestManifest), base.Add(2*time.Second))
	makeListedRun(t, workspace, ".pending-hidden", "output.csv", encodeListedManifest(t, latestManifest), base.Add(time.Minute))
	makeListedRun(t, otherWorkspace, "run-other", "output.csv", nil, base.Add(time.Minute))

	first := listRunsForTest(t, workspace, 0, 2)
	if len(first.Runs) != 2 || first.NextOffset != 2 {
		t.Fatalf("first page: %+v", first)
	}
	newest, fallback := first.Runs[0], first.Runs[1]
	if newest.ID != "run-new" || newest.OutputPath != latest || !newest.Available || newest.CompletedAt != latestManifest.CompletedAt || !reflect.DeepEqual(newest.Manifest, &latestManifest) {
		t.Fatalf("newest run: %+v", newest)
	}
	if fallback.ID != "run-malformed" || fallback.OutputPath != malformed || !fallback.Available || fallback.CompletedAt != base.Add(time.Second).Format(time.RFC3339Nano) || fallback.Manifest != nil {
		t.Fatalf("malformed entry: %+v", fallback)
	}
	last := listRunsForTest(t, workspace, first.NextOffset, 2)
	if len(last.Runs) != 1 || last.Runs[0].OutputPath != legacy || !last.Runs[0].Available || last.Runs[0].Manifest != nil || last.NextOffset != 0 {
		t.Fatalf("last page: %+v", last)
	}
	if page := listRunsForTest(t, workspace, 100, 1); len(page.Runs) != 0 || page.NextOffset != 0 {
		t.Fatalf("out-of-range page: %+v", page)
	}
}

func TestListRunsIsolatesUnsafeAndMissingArtifacts(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	when := time.Now()
	outsideOutput := filepath.Join(outside, "outside.csv")
	if err := os.WriteFile(outsideOutput, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(workspace, ".studio", "runs")
	missing := listedManifest("run-missing", when)
	makeListedRun(t, workspace, "run-missing", "", encodeListedManifest(t, missing), when)
	forged := listedManifest("run-forged", when)
	forged.OutputPath = outsideOutput
	forgedFallback := makeListedRun(t, workspace, "run-forged", "output.csv", encodeListedManifest(t, forged), when)
	substitution := listedManifest("run-substitution", when)
	makeListedRun(t, workspace, "run-substitution", "", encodeListedManifest(t, substitution), when)
	makeListedRun(t, workspace, "run-manifest-link", "output.csv", nil, when)
	symlinkForRunListTest(t, outsideOutput, filepath.Join(root, "run-substitution", "output.csv"))
	manifestTarget := filepath.Join(outside, runManifestFile)
	if err := os.WriteFile(manifestTarget, encodeListedManifest(t, listedManifest("run-manifest-link", when)), 0600); err != nil {
		t.Fatal(err)
	}
	symlinkForRunListTest(t, manifestTarget, filepath.Join(root, "run-manifest-link", runManifestFile))
	symlinkForRunListTest(t, outside, filepath.Join(root, "run-directory-link"))

	page := listRunsForTest(t, workspace, 0, 20)
	if len(page.Runs) != 4 {
		t.Fatalf("unexpected entries: %+v", page)
	}
	byID := make(map[string]runEntry)
	for _, run := range page.Runs {
		byID[run.ID] = run
	}
	if run := byID["run-missing"]; run.Available || run.Manifest == nil || run.OutputPath != missing.OutputPath {
		t.Fatalf("missing output: %+v", run)
	}
	if run := byID["run-substitution"]; run.Available || run.Manifest == nil {
		t.Fatalf("symlink output: %+v", run)
	}
	if run := byID["run-manifest-link"]; !run.Available || run.Manifest != nil {
		t.Fatalf("symlink manifest: %+v", run)
	}
	if run := byID["run-forged"]; !run.Available || run.Manifest != nil || run.OutputPath != forgedFallback {
		t.Fatalf("forged manifest: %+v", run)
	}

	linkedWorkspace := t.TempDir()
	symlinkForRunListTest(t, filepath.Join(workspace, ".studio"), filepath.Join(linkedWorkspace, ".studio"))
	if resp := request(t, "listRuns", Params{Workspace: linkedWorkspace}); resp.OK {
		t.Fatalf("listed history through symlinked parent: %+v", resp)
	}
}

func TestListRunsFitsProtocolAndResumesAtNextOffset(t *testing.T) {
	workspace := t.TempDir()
	when := time.Now()
	for _, id := range []string{"run-a", "run-b", "run-c"} {
		manifest := listedManifest(id, when)
		manifest.WorkflowName = strings.Repeat("<", MaxFrameBytes/12+256)
		makeListedRun(t, workspace, id, "output.csv", encodeListedManifest(t, manifest), when)
	}
	var input, output bytes.Buffer
	req := Request{Version: Version, ID: "list", Command: "listRuns", Params: Params{Workspace: workspace, Limit: 3}}
	if err := json.NewEncoder(&input).Encode(req); err != nil {
		t.Fatal(err)
	}
	if err := Serve(context.Background(), &input, &output); err != nil {
		t.Fatal(err)
	}
	if output.Len() > MaxFrameBytes {
		t.Fatalf("frame size %d exceeds %d", output.Len(), MaxFrameBytes)
	}
	var response struct {
		OK     bool    `json:"ok"`
		Result runPage `json:"result"`
	}
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || len(response.Result.Runs) != 1 || response.Result.Runs[0].ID != "run-c" || response.Result.Runs[0].Manifest == nil || response.Result.NextOffset != 1 {
		t.Fatalf("invalid first page (success %t, rows %d, offset %d)", response.OK, len(response.Result.Runs), response.Result.NextOffset)
	}
	page := listRunsForTest(t, workspace, response.Result.NextOffset, 1)
	if len(page.Runs) != 1 || page.Runs[0].ID != "run-b" || page.NextOffset != 2 {
		t.Fatalf("resume returned %d rows with offset %d", len(page.Runs), page.NextOffset)
	}
}

func TestListRunsFallsBackForInvalidWorkflowPath(t *testing.T) {
	workspace := t.TempDir()
	const id = "run-forged-workflow"
	when := time.Now()
	manifest := listedManifest(id, when)
	manifest.WorkflowPath = `C:\outside.yaml`
	output := makeListedRun(t, workspace, id, "output.csv", encodeListedManifest(t, manifest), when)
	page := listRunsForTest(t, workspace, 0, 1)
	if len(page.Runs) != 1 || !page.Runs[0].Available || page.Runs[0].OutputPath != output || page.Runs[0].Manifest != nil {
		t.Fatalf("expected fallback entry, got %+v", page)
	}
}

func listRunsForTest(t *testing.T, workspace string, offset, limit int) runPage {
	t.Helper()
	resp := request(t, "listRuns", Params{Workspace: workspace, Offset: offset, Limit: limit})
	if !resp.OK {
		t.Fatalf("listRuns failed: %+v", resp.Error)
	}
	return resp.Result.(runPage)
}

func makeListedRun(t *testing.T, workspace, id, output string, manifest []byte, mtime time.Time) string {
	t.Helper()
	dir := filepath.Join(workspace, ".studio", "runs", id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if output != "" {
		if err := os.WriteFile(filepath.Join(dir, output), []byte("id\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if manifest != nil {
		if err := os.WriteFile(filepath.Join(dir, runManifestFile), manifest, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(dir, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(filepath.Join(".studio", "runs", id, output))
}

func listedManifest(id string, when time.Time) runManifest {
	digest := strings.Repeat("a", 64)
	return runManifest{
		Version: runManifestVersion, CompletedAt: when.UTC().Format(time.RFC3339Nano),
		WorkflowPath: "workflow.yaml", WorkflowName: "History", YAMLSHA256: digest,
		Inputs:     []runManifestInput{{Name: "input", Path: "sample.csv", SHA256: digest}},
		OutputPath: ".studio/runs/" + id + "/output.csv", OutputSHA256: digest,
	}
}

func encodeListedManifest(t *testing.T, manifest runManifest) []byte {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func symlinkForRunListTest(t *testing.T, old, new string) {
	t.Helper()
	if err := os.Symlink(old, new); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}
