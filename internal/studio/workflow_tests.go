package studio

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Sepiolina/migo/engine/wftest"
	"github.com/Sepiolina/migo/engine/workflow"
)

const workflowTestTimeout = 30 * time.Second

var errWorkflowTestsTooLarge = errors.New("Workflow test results exceed the 1 MiB response limit")

type workflowTestsResult struct {
	WorkflowPath string                   `json:"workflowPath"`
	Revision     string                   `json:"revision"`
	Suites       []workflowTestSuite      `json:"suites"`
	Diagnostics  []workflowTestDiagnostic `json:"diagnostics,omitempty"`
}

type workflowTestDiagnostic struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type workflowTestSuite struct {
	Path  string             `json:"path"`
	Name  string             `json:"name"`
	Cases []workflowTestCase `json:"cases"`
}

type workflowTestCase struct {
	Name          string             `json:"name"`
	ExpectedError string             `json:"expectedError,omitempty"`
	ActualError   string             `json:"actualError,omitempty"`
	Outcome       string             `json:"outcome,omitempty"`
	DurationMS    *int64             `json:"durationMs,omitempty"`
	Error         string             `json:"error,omitempty"`
	Mismatch      *wftest.Difference `json:"mismatch,omitempty"`
}

func workflowTests(ctx context.Context, p Params, run bool) (workflowTestsResult, error) {
	ctx, cancel := context.WithTimeout(ctx, workflowTestTimeout)
	defer cancel()
	var result workflowTestsResult
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if p.WorkflowPath == "" {
		return result, errors.New("A saved workflow path is required")
	}
	root, err := localPath(p.Workspace, ".")
	if err != nil {
		return result, err
	}
	target, err := localPath(root, p.WorkflowPath)
	if err != nil {
		return result, err
	}
	source, err := readTestWorkflow(target)
	if err != nil {
		return result, err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return result, err
	}
	result = workflowTestsResult{
		WorkflowPath: filepath.ToSlash(relative),
		Revision:     fmt.Sprintf("%x", sha256.Sum256(source)),
		Suites:       []workflowTestSuite{},
	}
	var wf *workflow.Workflow
	if run {
		if p.Revision != "" && p.Revision != result.Revision {
			return result, errors.New("The saved workflow changed before testing")
		}
		wf, err = workflow.Parse(source)
		if err != nil {
			return result, err
		}
	}
	selected := p.Path != ""
	paths := []string{root}
	if selected {
		if !strings.HasSuffix(p.Path, wftest.Suffix) {
			return result, fmt.Errorf("Expected a %s suite", wftest.Suffix)
		}
		path := p.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		paths = []string{path}
	} else {
		paths, err = wftest.FindContext(ctx, paths)
		if err != nil {
			return result, err
		}
	}

	// Reserve room for the protocol envelope. Measure suites individually so
	// an oversized discovery or test result stops growing immediately.
	remaining := MaxFrameBytes - 4096 - 6*len(result.WorkflowPath)
	addToFrame := func(value any) error {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		remaining -= len(encoded) + 1
		if remaining < 0 {
			return errWorkflowTestsTooLarge
		}
		return nil
	}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return result, err
		}
		relative = filepath.ToSlash(relative)
		file, err := wftest.LoadWorkspace(root, path)
		var workflowPath string
		if err == nil {
			workflowPath, err = file.WorkflowPath(root)
		}
		if err != nil {
			if selected {
				return result, err
			}
			diagnostic := workflowTestDiagnostic{Path: relative, Message: err.Error()}
			if err := addToFrame(diagnostic); err != nil {
				return result, err
			}
			result.Diagnostics = append(result.Diagnostics, diagnostic)
			continue
		}
		if workflowPath != target {
			if selected {
				return result, errors.New("The suite belongs to another workflow")
			}
			continue
		}
		suite := workflowTestSuite{
			Path:  relative,
			Name:  strings.TrimSuffix(filepath.Base(path), wftest.Suffix),
			Cases: make([]workflowTestCase, len(file.Tests)),
		}
		for i, c := range file.Tests {
			suite.Cases[i] = workflowTestCase{Name: c.Name, ExpectedError: c.Error}
		}
		if run {
			for i, outcome := range file.RunWorkflow(ctx, root, wf) {
				c := &suite.Cases[i]
				duration := outcome.Duration.Milliseconds()
				c.DurationMS = &duration
				c.Outcome = "passed"
				c.ActualError = outcome.ActualError
				c.Mismatch = outcome.Difference
				if outcome.Err != nil {
					c.Outcome = "failed"
					c.Error = outcome.Err.Error()
				}
			}
			if err := ctx.Err(); err != nil {
				return result, err
			}
		}
		if err := addToFrame(suite); err != nil {
			return result, err
		}
		result.Suites = append(result.Suites, suite)
	}
	return result, ctx.Err()
}

func readTestWorkflow(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxFrameBytes {
		return nil, errors.New("Saved workflow must be a regular file of at most 1 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	source, err := io.ReadAll(io.LimitReader(f, MaxFrameBytes+1))
	if err != nil {
		return nil, err
	}
	if len(source) > MaxFrameBytes {
		return nil, errors.New("Saved workflow must be at most 1 MiB")
	}
	return source, nil
}
