// Package studio adapts the production workflow engine to the desktop protocol.
package studio

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
	"miservice/internal/deploy"
	"miservice/internal/sheet"
	"miservice/internal/workflow"
)

const Version = 1
const MaxFrameBytes = 1 << 20
const maxWorkbookBytes = 25 << 20

type Request struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Command string `json:"command"`
	Params  Params `json:"params"`
}
type Params struct {
	Build     deploy.Options    `json:"build"`
	Workspace string            `json:"workspace"`
	YAML      string            `json:"yaml"`
	Path      string            `json:"path"`
	Sheet     string            `json:"sheet"`
	Offset    int               `json:"offset"`
	Limit     int               `json:"limit"`
	Inputs    map[string]string `json:"inputs"`
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
		resp.Error = &Failure{Code: "operation_failed", Message: err.Error()}
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
		return map[string]any{"version": Version, "commands": []string{"capabilities", "validate", "inspectWorkbook", "run", "buildPackage"}, "offlineOnly": true, "maxFrameBytes": MaxFrameBytes}, nil
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
	case "run":
		return run(ctx, p)
	case "buildPackage":
		return deploy.Build(ctx, p.Workspace, p.Build)
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
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Size() > maxBytes {
		return errors.New("Workbook must be a regular file of at most 25 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	var magic [2]byte
	_, err = io.ReadFull(f, magic[:])
	f.Close()
	if err != nil {
		return err
	}
	if string(magic[:]) == "PK" {
		z, err := zip.OpenReader(path)
		if err != nil {
			return err
		}
		defer z.Close()
		var size uint64
		for _, entry := range z.File {
			if entry.UncompressedSize64 > 64<<20 {
				return errors.New("Workbook exceeds 64 MiB expanded limit")
			}
			size += entry.UncompressedSize64
			if size > 64<<20 {
				return errors.New("Workbook exceeds 64 MiB expanded limit")
			}
		}
	}
	return nil
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
	rows, err := book.Rows(preview.Sheet)
	if err != nil {
		return preview, err
	}
	preview.TotalRows = len(rows)
	preview.Offset = p.Offset
	limit := p.Limit
	if limit == 0 {
		limit = 100
	}
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

// The production input reader intentionally honors declared sheet ranges. The
// desktop XLSX viewer reads actual cells, including writer output beyond a stale
// dimension tag; it must not silently hide cells present in the exported file.
type previewXLSX struct{ *excelize.File }

func (b previewXLSX) SheetNames() []string                 { return b.GetSheetList() }
func (b previewXLSX) Rows(name string) ([][]string, error) { return b.GetRows(name) }
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

func runWithInspect(ctx context.Context, p Params, inspectOutput func(string, Params) (Preview, error)) (any, error) {
	wf, err := workflow.Parse([]byte(p.YAML))
	if err != nil {
		return nil, err
	}
	root, err := localPath(p.Workspace, ".")
	if err != nil {
		return nil, err
	}
	inputs := make(map[string]string, len(p.Inputs))
	for _, input := range wf.Inputs {
		if p.Inputs[input.Name] == "" {
			return nil, fmt.Errorf("Missing input %q", input.Name)
		}
		path, err := localPath(root, p.Inputs[input.Name])
		if err != nil {
			return nil, err
		}
		if err := checkWorkbook(path); err != nil {
			return nil, err
		}
		inputs[input.Name] = path
	}
	// Refuse symlinked artifact parents; MkdirAll does not establish containment.
	// Create each directory only after verifying its existing parent is local.
	checked := root
	for _, name := range []string{".studio", "runs"} {
		next := filepath.Join(checked, name)
		if err := os.Mkdir(next, 0700); err != nil && !os.IsExist(err) {
			return nil, err
		}
		checked, err = localPath(root, next)
		if err != nil {
			return nil, err
		}
	}
	dir, err := os.MkdirTemp(checked, "run-")
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(dir)
		}
	}()
	output := filepath.Join(dir, "output.xlsx")
	rt := workflow.Runtime{HTTP: &http.Client{Transport: offlineTransport{}}, SQL: func(context.Context, string, string, []any) (*workflow.Table, error) {
		return nil, errors.New("SQL is disabled in desktop offline runs")
	}}
	if err := wf.Run(ctx, rt, inputs, output); err != nil {
		return nil, err
	}
	f, err := os.Open(output)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, output)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"path": filepath.ToSlash(rel), "sha256": hex.EncodeToString(h.Sum(nil))}
	preview, err := inspectOutput(output, Params{Limit: 100})
	if err != nil {
		result["previewWarning"] = err.Error()
	} else if encoded, err := json.Marshal(preview); err != nil || len(encoded) > MaxFrameBytes/2 {
		result["previewWarning"] = "Workbook preview is too large to display"
	} else {
		result["preview"] = preview
	}
	success = true
	return result, nil
}
