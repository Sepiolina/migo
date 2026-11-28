// Package studio adapts the production workflow engine to the desktop protocol.
package studio

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Sepiolina/migo/engine/mock"
	"github.com/Sepiolina/migo/engine/sheet"
	"github.com/Sepiolina/migo/engine/wftest"
	"github.com/Sepiolina/migo/engine/workflow"
	"github.com/xuri/excelize/v2"
	"miservice/internal/deploy"
)

const Version = 1
const MaxFrameBytes = 1 << 20
const maxWorkbookBytes = 25 << 20
const maxWorkbookExpandedBytes = 64 << 20

type Request struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Command string `json:"command"`
	Params  Params `json:"params"`
}
type Params struct {
	OmitPreview  bool              `json:"omitPreview,omitempty"`
	Compare      bool              `json:"compare,omitempty"`
	Build        deploy.Options    `json:"build"`
	Workspace    string            `json:"workspace"`
	YAML         string            `json:"yaml"`
	WorkflowPath string            `json:"workflowPath,omitempty"`
	Revision     string            `json:"revision,omitempty"`
	Path         string            `json:"path"`
	Sheet        string            `json:"sheet"`
	Offset       int               `json:"offset"`
	Limit        int               `json:"limit"`
	ColumnOffset int               `json:"columnOffset"`
	ColumnLimit  int               `json:"columnLimit"`
	Inputs       map[string]string `json:"inputs"`
	Step         *int              `json:"step"`            // trace: the step whose rows are returned
	Skip         []int             `json:"skip"`            // trace: steps to leave out
	Mocks        string            `json:"mocks,omitempty"` // run, trace, saveTest: a workspace SQL and REST mocks file
	Name         string            `json:"name,omitempty"`  // saveTest: the new test name
}
type Failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Response struct {
	Version int      `json:"version"`
	ID      string   `json:"id"`
	OK      bool     `json:"ok"`
	Result  any      `json:"result,omitempty"`
	Error   *Failure `json:"error,omitempty"`
}
type Input struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}
type Validation struct {
	Name   string   `json:"name"`
	Inputs []Input  `json:"inputs"`
	Sheets []string `json:"sheets"`
}
type Preview struct {
	Sheets    []string   `json:"sheets"`
	Sheet     string     `json:"sheet"`
	Rows      [][]string `json:"rows"`
	Offset    int        `json:"offset"`
	TotalRows int        `json:"totalRows"`
	Truncated bool       `json:"truncated"`
}

// Serve processes bounded JSON lines sequentially. Process supervision supplies
// cancellation; ordinary logs never enter stdout and corrupt protocol framing.
func Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 4096), MaxFrameBytes)
	enc := json.NewEncoder(out)
	for scan.Scan() {
		var req Request
		var resp Response
		if err := json.Unmarshal(scan.Bytes(), &req); err != nil {
			resp = Response{Version: Version, Error: &Failure{Code: "invalid_request", Message: "Expected a JSON request"}}
		} else {
			resp = Handle(ctx, req)
		}
		encoded, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		if len(encoded) > MaxFrameBytes {
			resp = Response{Version: Version, ID: req.ID, Error: &Failure{Code: "response_too_large", Message: "Response exceeds 1 MiB; request fewer preview rows"}}
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return scan.Err()
}
func Handle(ctx context.Context, req Request) Response {
	resp := Response{Version: Version, ID: req.ID}
	if req.Version != Version {
		resp.Error = &Failure{Code: "unsupported_version", Message: "Expected protocol version 1"}
		return resp
	}
	result, err := execute(ctx, req)
	if err != nil {
		code := "operation_failed"
		if errors.Is(err, errWorkbookPageTooLarge) || errors.Is(err, errRunPageTooLarge) || errors.Is(err, errWorkflowTestsTooLarge) {
			code = "response_too_large"
		}
		resp.Error = &Failure{Code: code, Message: err.Error()}
		return resp
	}
	resp.OK = true
	resp.Result = result
	return resp
}
func execute(ctx context.Context, req Request) (any, error) {
	p := req.Params
	switch req.Command {
	case "capabilities":
		return map[string]any{"version": Version, "commands": []string{"capabilities", "validate", "inspectWorkbook", "inspectWorkbookPage", "run", "saveTest", "listRuns", "trace", "buildPackage", "discoverTests", "runTests"}, "offlineOnly": true, "maxFrameBytes": MaxFrameBytes}, nil
	case "validate":
		wf, err := workflow.Parse([]byte(p.YAML))
		if err != nil {
			return nil, err
		}
		v := Validation{Name: wf.Name, Inputs: []Input{}, Sheets: []string{}}
		for _, i := range wf.Inputs {
			v.Inputs = append(v.Inputs, Input{Name: i.Name, Label: i.Label})
		}
		for _, s := range wf.Sheets {
			v.Sheets = append(v.Sheets, s.Name)
		}
		return v, nil
	case "inspectWorkbook":
		path, err := localPath(p.Workspace, p.Path)
		if err != nil {
			return nil, err
		}
		return inspect(path, p)
	case "inspectWorkbookPage":
		path, err := localPath(p.Workspace, p.Path)
		if err != nil {
			return nil, err
		}
		return inspectWorkbookPage(ctx, path, req)
	case "run":
		return run(ctx, p)
	case "listRuns":
		return listRuns(ctx, req)
	case "saveTest":
		return saveTest(ctx, p)
	case "trace":
		return trace(ctx, p)
	case "buildPackage":
		return deploy.Build(ctx, p.Workspace, p.Build)
	case "discoverTests", "runTests":
		return workflowTests(ctx, p, req.Command == "runTests")
	default:
		return nil, fmt.Errorf("Unknown command %q", req.Command)
	}
}

// localPath resolves symlinks before checking containment. The native host must
// authorize workspace selection; this helper additionally scopes runner inputs.
func localPath(workspace, path string) (string, error) {
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("Path is outside the workspace")
	}
	return target, nil
}
func checkWorkbook(path string) error {
	return checkWorkbookWithLimit(path, maxWorkbookBytes)
}
func checkWorkbookWithLimit(path string, maxBytes int64) error {
	f, err := openWorkbookFile(path, maxBytes)
	if err != nil {
		return err
	}
	defer f.Close()
	_, _, err = workbookArchive(f, maxBytes)
	return err
}

var errWorkbookFileSize = errors.New("Workbook must be a regular file of at most 25 MiB")

func openWorkbookFile(path string, maxBytes int64) (*os.File, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > maxBytes {
		return nil, errWorkbookFileSize
	}
	return os.Open(path)
}

func workbookArchive(f *os.File, maxBytes int64) (*zip.Reader, int64, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	if !st.Mode().IsRegular() || st.Size() > maxBytes {
		return nil, 0, errWorkbookFileSize
	}
	var magic [2]byte
	_, err = f.ReadAt(magic[:], 0)
	if err != nil {
		return nil, 0, err
	}
	if string(magic[:]) != "PK" {
		return nil, st.Size(), nil
	}
	z, err := zip.NewReader(f, st.Size())
	if err != nil {
		return nil, 0, err
	}
	var size uint64
	for _, entry := range z.File {
		if entry.UncompressedSize64 > maxWorkbookExpandedBytes-size {
			return nil, 0, errors.New("Workbook exceeds 64 MiB expanded limit")
		}
		size += entry.UncompressedSize64
	}
	return z, st.Size(), nil
}
func inspect(path string, p Params) (Preview, error) {
	return inspectWithLimit(path, p, maxWorkbookBytes)
}
func inspectWithLimit(path string, p Params, maxBytes int64) (Preview, error) {
	preview := Preview{Rows: [][]string{}}
	if p.Offset < 0 || p.Limit < 0 || p.Limit > 1000 {
		return preview, errors.New("Offset must be nonnegative and limit at most 1000")
	}
	if err := checkWorkbookWithLimit(path, maxBytes); err != nil {
		return preview, err
	}
	book, err := openPreview(path)
	if err != nil {
		return preview, err
	}
	defer book.Close()
	preview.Sheets = book.SheetNames()
	if len(preview.Sheets) == 0 {
		return preview, errors.New("Workbook has no sheets")
	}
	preview.Sheet = p.Sheet
	if preview.Sheet == "" {
		preview.Sheet = preview.Sheets[0]
	}
	found := false
	for _, name := range preview.Sheets {
		if name == preview.Sheet {
			found = true
		}
	}
	if !found {
		return preview, fmt.Errorf("Unknown sheet %q", preview.Sheet)
	}
	preview.Offset = p.Offset
	limit := p.Limit
	if limit == 0 {
		limit = 100
	}
	if xlsx, ok := book.(previewXLSX); ok {
		preview.Rows, preview.TotalRows, err = xlsx.window(preview.Sheet, p.Offset, limit)
		preview.Truncated = len(preview.Rows) > 0 && preview.TotalRows-p.Offset > len(preview.Rows)
		return preview, err
	}
	rows, err := book.Rows(preview.Sheet)
	if err != nil {
		return preview, err
	}
	preview.TotalRows = len(rows)
	if p.Offset < len(rows) {
		end := p.Offset + limit
		if end > len(rows) {
			end = len(rows)
		}
		preview.Rows = rows[p.Offset:end]
		preview.Truncated = end < len(rows)
	}
	return preview, nil
}

func openPreview(path string) (sheet.Workbook, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	var magic [2]byte
	_, err = io.ReadFull(f, magic[:])
	f.Close()
	if err != nil {
		return nil, err
	}
	if string(magic[:]) == "PK" {
		b, err := excelize.OpenFile(path, excelize.Options{UnzipSizeLimit: 64 << 20, UnzipXMLSizeLimit: 8 << 20})
		if err != nil {
			return nil, err
		}
		return previewXLSX{b}, nil
	}
	return sheet.Open(path)
}

type offlineTransport struct{}

func (offlineTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("External HTTP is disabled in desktop offline runs")
}
func run(ctx context.Context, p Params) (any, error) {
	return runWithInspect(ctx, p, inspect)
}

// resolveInputs checks every declared input is bound to a file in the workspace.
func resolveInputs(wf *workflow.Workflow, root string, bound map[string]string) (map[string]string, error) {
	inputs := make(map[string]string, len(bound))
	for _, input := range wf.Inputs {
		if bound[input.Name] == "" {
			return nil, fmt.Errorf("Missing input %q", input.Name)
		}
		path, err := localPath(root, bound[input.Name])
		if err != nil {
			return nil, err
		}
		if err := checkWorkbook(path); err != nil {
			return nil, err
		}
		inputs[input.Name] = path
	}
	return inputs, nil
}

// offline is the desktop runtime: SQL and external HTTP are unavailable.
func offline() workflow.Runtime {
	return workflow.Runtime{HTTP: &http.Client{Transport: offlineTransport{}}, SQL: func(context.Context, string, string, []any) (*workflow.Table, error) {
		return nil, errors.New("SQL is disabled in desktop offline runs")
	}}
}

// runtimeFor is the runtime of a desktop run: the answers in the workspace's
// mocks file when p.Mocks names one that exists, otherwise offline. It also
// returns the mocks file's path ("" when there is none).
func runtimeFor(root string, p Params) (workflow.Runtime, string, error) {
	if p.Mocks == "" {
		return offline(), "", nil
	}
	path, err := localPath(root, p.Mocks)
	if errors.Is(err, os.ErrNotExist) {
		return offline(), "", nil
	}
	if err != nil {
		return workflow.Runtime{}, "", err
	}
	set, err := mock.Load(path)
	if err != nil {
		return workflow.Runtime{}, "", err
	}
	rt, err := set.Runtime(nil)
	return rt, path, err
}

// saveTest keeps a run as a workflow test: it runs the saved workflow at
// p.Path on the bound sample files and adds a case, with the output as its
// expected file, to tests/<workflow>.test.yaml in the workspace.
func saveTest(ctx context.Context, p Params) (any, error) {
	root, err := localPath(p.Workspace, ".")
	if err != nil {
		return nil, err
	}
	workflowPath, err := localPath(root, p.Path)
	if err != nil {
		return nil, err
	}
	wf, err := workflow.Load(workflowPath)
	if err != nil {
		return nil, err
	}
	inputs, err := resolveInputs(wf, root, p.Inputs)
	if err != nil {
		return nil, err
	}
	_, mocks, err := runtimeFor(root, p)
	if err != nil {
		return nil, err
	}
	// Refuse a symlinked tests folder, as for run artifacts.
	dir := filepath.Join(root, "tests")
	if err := os.Mkdir(dir, 0o755); err != nil && !os.IsExist(err) {
		return nil, err
	}
	if dir, err = localPath(root, dir); err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(workflowPath), ".yaml"), ".yml")
	file := filepath.Join(dir, base+wftest.Suffix)
	if info, err := os.Lstat(file); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("The test file is not a regular file")
	}
	expected, err := wftest.Add(ctx, file, workflowPath, p.Name, inputs, mocks)
	if err != nil {
		return nil, err
	}
	rel := func(path string) string {
		r, _ := filepath.Rel(root, path)
		return filepath.ToSlash(r)
	}
	return map[string]any{"file": rel(file), "expected": rel(expected)}, nil
}

// maxTraceRows bounds the rows a trace returns for its focused step.
const maxTraceRows = 500

// trace runs the workflow step by step without writing anything and returns
// each top-level step's column profile, plus rows for the focused step.
func trace(ctx context.Context, p Params) (any, error) {
	wf, err := workflow.Parse([]byte(p.YAML))
	if err != nil {
		return nil, err
	}
	root, err := localPath(p.Workspace, ".")
	if err != nil {
		return nil, err
	}
	inputs, err := resolveInputs(wf, root, p.Inputs)
	if err != nil {
		return nil, err
	}
	opt := workflow.TraceOptions{Compare: p.Compare, Focus: -1, Limit: p.Limit, Skip: p.Skip}
	if p.Step != nil {
		opt.Focus = *p.Step
	}
	if opt.Limit <= 0 || opt.Limit > maxTraceRows {
		opt.Limit = 200
	}
	rt, mocks, err := runtimeFor(root, p)
	if err != nil {
		return nil, err
	}
	steps, err := wf.Trace(ctx, rt, inputs, opt)
	if err != nil {
		return nil, err
	}
	// Keep the frame under the protocol limit: drop rows before profiles.
	for {
		encoded, err := json.Marshal(steps)
		if err != nil {
			return nil, err
		}
		if len(encoded) < MaxFrameBytes-4096 || opt.Focus < 0 || opt.Focus >= len(steps) {
			break
		}
		focused := &steps[opt.Focus]
		if c := focused.Comparison; c != nil && (len(c.Before) > 0 || len(c.Removed) > 0) {
			c.Before = c.Before[:len(c.Before)/2]
			c.Removed = c.Removed[:len(c.Removed)/2]
			c.RemovedPositions = c.RemovedPositions[:len(c.Removed)]
			if len(c.Changed) > len(c.Before) {
				c.Changed = c.Changed[:len(c.Before)]
			}
			continue
		}
		if len(focused.Rows) == 0 {
			break
		}
		rows := focused.Rows
		steps[opt.Focus].Rows = rows[:len(rows)/2]
	}
	return map[string]any{"steps": steps, "format": wf.Format, "mocks": mocks != ""}, nil
}

func runWithInspect(ctx context.Context, p Params, inspectOutput func(string, Params) (Preview, error)) (any, error) {
	wf, err := workflow.Parse([]byte(p.YAML))
	if err != nil {
		return nil, err
	}
	root, err := localPath(p.Workspace, ".")
	if err != nil {
		return nil, err
	}
	inputs, err := resolveInputs(wf, root, p.Inputs)
	if err != nil {
		return nil, err
	}
	rt, _, err := runtimeFor(root, p)
	if err != nil {
		return nil, err
	}
	runID, manifest, err := persistRun(ctx, root, wf, p, inputs, func(output string) error {
		return wf.Run(ctx, rt, inputs, output)
	})
	if err != nil {
		return nil, err
	}
	result := map[string]any{"path": manifest.OutputPath, "sha256": manifest.OutputSHA256, "runId": runID}
	if p.OmitPreview {
		return result, nil
	}
	preview, err := inspectOutput(filepath.Join(root, filepath.FromSlash(manifest.OutputPath)), Params{Limit: 100})
	if err != nil {
		result["previewWarning"] = err.Error()
	} else if encoded, err := json.Marshal(preview); err != nil || len(encoded) > MaxFrameBytes/2 {
		result["previewWarning"] = "Workbook preview is too large to display"
	} else {
		result["preview"] = preview
	}
	return result, nil
}
