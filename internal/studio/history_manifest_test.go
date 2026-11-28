package studio

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sepiolina/migo/engine/workflow"
)

func historyRunFixture(t *testing.T) (Params, *workflow.Workflow, map[string]string) {
	t.Helper()
	root := t.TempDir()
	examples := filepath.Join(fixtureRoot(t), "examples")
	yamlPath := filepath.Join(root, "workflow.yaml")
	inputPath := filepath.Join(root, "parts.xlsx")
	for source, destination := range map[string]string{"parts-summary.yaml": yamlPath, "parts.xlsx": inputPath} {
		data, err := os.ReadFile(filepath.Join(examples, source))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Resolve the temp directory just as the production runner does.
	root, err := localPath(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	yaml, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	p := Params{Workspace: root, YAML: string(yaml), Inputs: map[string]string{"parts": "parts.xlsx"}, OmitPreview: true}
	wf, err := workflow.Parse(yaml)
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := resolveInputs(wf, root, p.Inputs)
	if err != nil {
		t.Fatal(err)
	}
	return p, wf, inputs
}

func TestRunPublishesManifestWithArtifact(t *testing.T) {
	p, wf, inputs := historyRunFixture(t)
	p.WorkflowPath = "workflow.yaml"
	response := Handle(context.Background(), Request{Version: Version, Command: "run", Params: p})
	if !response.OK {
		t.Fatal(response.Error)
	}
	result := response.Result.(map[string]any)
	id := result["runId"].(string)
	outputPath := result["path"].(string)
	if !strings.HasPrefix(id, "run-") || filepath.Base(filepath.Dir(outputPath)) != id {
		t.Fatalf("unexpected run result %+v", result)
	}
	manifestPath := filepath.Join(p.Workspace, filepath.Dir(outputPath), runManifestFile)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest runManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 1 || manifest.WorkflowPath != p.WorkflowPath || manifest.WorkflowName != wf.Name ||
		manifest.YAMLSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(p.YAML))) || manifest.Uncertain ||
		manifest.OutputPath != outputPath || manifest.OutputSHA256 != result["sha256"] {
		t.Fatalf("unexpected manifest %+v", manifest)
	}
	if _, err := time.Parse(time.RFC3339, manifest.CompletedAt); err != nil {
		t.Fatal(err)
	}
	input, err := os.ReadFile(inputs["parts"])
	if err != nil {
		t.Fatal(err)
	}
	want := runManifestInput{Name: "parts", Path: "parts.xlsx", SHA256: fmt.Sprintf("%x", sha256.Sum256(input))}
	if len(manifest.Inputs) != 1 || manifest.Inputs[0] != want {
		t.Fatalf("unexpected inputs %+v, want %+v", manifest.Inputs, want)
	}
}

func TestRunMarksInputChanges(t *testing.T) {
	for _, change := range []string{"write", "remove", "retarget"} {
		t.Run(change, func(t *testing.T) {
			p, wf, inputs := historyRunFixture(t)
			input := inputs["parts"]
			original, err := os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}
			if change == "retarget" {
				target := filepath.Join(p.Workspace, "second.xlsx")
				if err := os.WriteFile(target, original, 0600); err != nil {
					t.Fatal(err)
				}
				p.Inputs["parts"] = "bound.xlsx"
				if err := os.Symlink(input, filepath.Join(p.Workspace, "bound.xlsx")); err != nil {
					t.Skip(err)
				}
			}
			_, manifest, err := persistRun(context.Background(), p.Workspace, wf, p, inputs, func(output string) error {
				if err := wf.Run(context.Background(), offline(), inputs, output); err != nil {
					return err
				}
				switch change {
				case "write":
					return os.WriteFile(input, []byte("changed"), 0600)
				case "remove":
					return os.Remove(input)
				case "retarget":
					binding := filepath.Join(p.Workspace, "bound.xlsx")
					if err := os.Remove(binding); err != nil {
						return err
					}
					return os.Symlink(filepath.Join(p.Workspace, "second.xlsx"), binding)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf("%x", sha256.Sum256(original))
			if !manifest.Uncertain || len(manifest.Inputs) != 1 || !manifest.Inputs[0].Changed || manifest.Inputs[0].SHA256 != want {
				t.Fatalf("unexpected provenance %+v", manifest)
			}
		})
	}
}

func TestRunRecordsBoundInputAlias(t *testing.T) {
	p, _, inputs := historyRunFixture(t)
	const alias = "sample-alias.xlsx"
	symlinkForRunListTest(t, inputs["parts"], filepath.Join(p.Workspace, alias))
	p.Inputs["parts"] = alias
	resp := request(t, "run", p)
	if !resp.OK {
		t.Fatal(resp.Error)
	}
	page := listRunsForTest(t, p.Workspace, 0, 1)
	if len(page.Runs) != 1 || page.Runs[0].Manifest == nil {
		t.Fatalf("expected published manifest, got %+v", page)
	}
	data, err := os.ReadFile(inputs["parts"])
	if err != nil {
		t.Fatal(err)
	}
	want := runManifestInput{Name: "parts", Path: alias, SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
	manifest := page.Runs[0].Manifest
	if manifest.Uncertain || len(manifest.Inputs) != 1 || manifest.Inputs[0] != want {
		t.Fatalf("unexpected provenance %+v, want input %+v", manifest, want)
	}
}

func TestRunPublicationFailureLeavesNoRun(t *testing.T) {
	for _, cause := range []string{"output", "manifest", "cancel"} {
		t.Run(cause, func(t *testing.T) {
			p, wf, inputs := historyRunFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, _, err := persistRun(ctx, p.Workspace, wf, p, inputs, func(output string) error {
				if cause == "output" {
					return nil // The generated file disappeared before it could be synchronized.
				}
				if err := os.WriteFile(output, []byte("output"), 0600); err != nil {
					return err
				}
				if cause == "cancel" {
					cancel()
					return nil
				}
				return os.Mkdir(filepath.Join(filepath.Dir(output), runManifestFile), 0700)
			})
			if err == nil || (cause == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("unexpected publication error %v", err)
			}
			dirs, err := os.ReadDir(filepath.Join(p.Workspace, ".studio", "runs"))
			if err != nil {
				t.Fatal(err)
			}
			if len(dirs) != 0 {
				t.Fatalf("failed runs left files: %v", dirs)
			}
		})
	}
}

func TestRunPublishesOverlappingRunsSeparately(t *testing.T) {
	p, wf, inputs := historyRunFixture(t)
	type result struct {
		id       string
		manifest runManifest
		err      error
	}
	var ready sync.WaitGroup
	ready.Add(2)
	start := make(chan struct{})
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			id, manifest, err := persistRun(context.Background(), p.Workspace, wf, p, inputs, func(output string) error {
				ready.Done()
				<-start
				return wf.Run(context.Background(), offline(), inputs, output)
			})
			results <- result{id, manifest, err}
		}()
	}
	staged := make(chan struct{})
	go func() {
		ready.Wait()
		close(staged)
	}()
	var earlyErr error
	select {
	case <-staged:
	case run := <-results:
		earlyErr = run.err
	case <-time.After(5 * time.Second):
		earlyErr = errors.New("runs failed to stage")
	}
	if earlyErr != nil {
		close(start)
		t.Fatal(earlyErr)
	}
	dirs, err := os.ReadDir(filepath.Join(p.Workspace, ".studio", "runs"))
	close(start)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 2 || !strings.HasPrefix(dirs[0].Name(), ".pending-") || !strings.HasPrefix(dirs[1].Name(), ".pending-") {
		t.Fatalf("expected two unpublished generations, got %v", dirs)
	}
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.id == second.id {
		t.Fatalf("unexpected run results %+v and %+v", first, second)
	}
	for _, run := range []result{first, second} {
		output := filepath.Join(p.Workspace, filepath.FromSlash(run.manifest.OutputPath))
		if _, err := os.Stat(output); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(output), runManifestFile)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRunRejectsInvalidWorkflowPath(t *testing.T) {
	p, _, _ := historyRunFixture(t)
	for _, path := range []string{"../outside.yaml", filepath.Join(p.Workspace, "workflow.yaml"), ".", "workflows/../escape.yaml", "C:/outside.yaml", `C:\outside.yaml`} {
		p.WorkflowPath = path
		response := Handle(context.Background(), Request{Version: Version, Command: "run", Params: p})
		if response.OK {
			t.Errorf("run accepted invalid workflow path %q", path)
		}
	}
}

func TestRunAcceptsMissingWorkflowPath(t *testing.T) {
	p, _, _ := historyRunFixture(t)
	p.WorkflowPath = "workflows/deleted.yaml"
	response := Handle(context.Background(), Request{Version: Version, Command: "run", Params: p})
	if !response.OK {
		t.Fatal(response.Error)
	}
	result := response.Result.(map[string]any)
	raw, err := os.ReadFile(filepath.Join(p.Workspace, filepath.Dir(result["path"].(string)), runManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	var manifest runManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.WorkflowPath != p.WorkflowPath {
		t.Fatalf("expected workflow path %q, got %q", p.WorkflowPath, manifest.WorkflowPath)
	}
}
