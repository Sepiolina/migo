package workflows

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"miservice/internal/httpx"
	"miservice/internal/jobs"
	"miservice/internal/workflow"
)

// Response bodies are structs (not maps) so key order matches the Express
// output byte for byte.
type successBody struct {
	Status      string `json:"status"`
	Message     string `json:"message"`
	FileName    string `json:"fileName"`
	DownloadURL string `json:"downloadUrl"`
	Bam         string `json:"bam"`
}

// request is a parsed upload: inputs matched to files, the owner and the
// output file name. done removes the uploaded files.
type request struct {
	inputs   map[string]string
	owner    string
	fileName string
	done     func()
}

// receive parses multipart "files" (one per input) and the owner field.
func (e *endpoint) receive(r *http.Request) (*request, error) {
	n := len(e.wf.Inputs)
	limit := MaxUploadMB
	if e.wf.Service.MaxUploadMB > 0 {
		limit = e.wf.Service.MaxUploadMB
	}
	up, err := httpx.ParseUpload(r, httpx.UploadOptions{
		Dir: e.uploadDir, FieldName: "files", MaxFiles: n, MaxFileMB: limit,
	})
	if err != nil {
		return nil, err
	}
	done := func() {
		// Uploaded inputs are only needed during this request. Parsing and
		// writing a workbook is the one heavy allocation in this service's
		// life; hand the memory back right away (Node: global.gc()).
		e.uploads.Discard(up.Paths()...) // deletes what is not registered in keep
		jobs.FreeMemory()
	}
	if len(up.Files) < n {
		done()
		labels := make([]string, n)
		for i, in := range e.wf.Inputs {
			labels[i] = in.Label
		}
		return nil, httpx.Fail(http.StatusBadRequest, fmt.Sprintf("Expected %d file%s (%s)", n, plural(n), strings.Join(labels, " & ")))
	}
	emp, ok := e.owner(up)
	if !ok {
		done()
		return nil, httpx.Fail(http.StatusBadRequest, fmt.Sprintf("Missing or invalid '%s' (employee number). Use letters, digits, '-' or '_' only.", e.wf.Service.OwnerField))
	}
	names := make([]string, len(up.Files))
	for i, f := range up.Files {
		names[i] = f.Name
	}
	inputs := map[string]string{}
	pick := e.wf.Assign(names)
	for i, in := range e.wf.Inputs {
		inputs[in.Name] = up.Files[pick[i]].Path
	}
	return &request{
		inputs:   inputs,
		owner:    emp,
		fileName: BuildOutputFilename(up.Files[pick[0]].Name, emp),
		done:     done,
	}, nil
}

// owner reads the owner field (bam by default, or the legacy "BOM Number").
// A workflow with service.owner_required: false accepts a missing one.
func (e *endpoint) owner(up *httpx.Upload) (string, bool) {
	field := e.wf.Service.OwnerField
	raw := up.Field(field)
	if raw == "" && field == "bam" {
		raw = up.Field("BOM Number")
	}
	if strings.TrimSpace(raw) == "" && !e.wf.Service.OwnerRequired {
		return "anonymous", true
	}
	return ParseEmployeeNumber(raw)
}

// upload handles POST /api/<name>/upload: multipart "files" (one per input)
// and "bam" (employee number).
func (e *endpoint) upload(w http.ResponseWriter, r *http.Request) error {
	req, err := e.receive(r)
	if err != nil {
		return err
	}
	defer req.done()
	outPath, err := e.generate(r, req.inputs, req.fileName, req.owner)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.latest = outPath
	e.mu.Unlock()

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return httpx.WriteJSON(w, http.StatusOK, successBody{
		Status:      "success",
		Message:     e.wf.Name + " processed successfully.",
		FileName:    req.fileName,
		DownloadURL: fmt.Sprintf("%s://%s/api/%s/download", scheme, r.Host, e.name),
		Bam:         req.owner,
	})
}

// run handles POST /api/<name>/run: the same form as upload, answered with
// the workbook itself. Nothing is kept, so concurrent clients never see each
// other's results; use it from scripts and other services.
func (e *endpoint) run(w http.ResponseWriter, r *http.Request) error {
	req, err := e.receive(r)
	if err != nil {
		return err
	}
	defer req.done()
	if err := e.gate.Acquire(r.Context()); err != nil {
		return err
	}
	defer e.gate.Release()
	tmp := e.store.NewTempPath()
	defer e.store.Discard(tmp)
	if err := e.wf.Run(r.Context(), e.rt, req.inputs, tmp); err != nil {
		return err
	}
	return serveWorkbook(w, r, tmp, req.fileName)
}

// Description is the JSON shape of a workflow in the catalog.
type Description struct {
	Name        string       `json:"name"`
	Route       string       `json:"route"`
	File        string       `json:"file"`
	Description string       `json:"description,omitempty"`
	Inputs      []inputDesc  `json:"inputs"`
	Sheets      []string     `json:"sheets"`
	Owner       ownerDesc    `json:"owner"`
	MaxUploadMB int          `json:"maxUploadMB"`
	Endpoints   endpointDesc `json:"endpoints"`
}
type inputDesc struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Match string `json:"match,omitempty"`
}
type ownerDesc struct {
	Field    string `json:"field"`
	Required bool   `json:"required"`
}
type endpointDesc struct {
	Upload   string `json:"upload"`
	Download string `json:"download"`
	Run      string `json:"run"`
}

func (e *endpoint) description() Description {
	d := Description{
		Name: e.wf.Name, Route: e.name, File: e.file + ".yaml",
		Description: e.wf.Service.Description,
		Owner:       ownerDesc{Field: e.wf.Service.OwnerField, Required: e.wf.Service.OwnerRequired},
		MaxUploadMB: MaxUploadMB,
		Inputs:      []inputDesc{},
		Endpoints: endpointDesc{
			Upload:   "/api/" + e.name + "/upload",
			Download: "/api/" + e.name + "/download",
			Run:      "/api/" + e.name + "/run",
		},
	}
	if e.wf.Service.MaxUploadMB > 0 {
		d.MaxUploadMB = e.wf.Service.MaxUploadMB
	}
	for _, in := range e.wf.Inputs {
		desc := inputDesc{Name: in.Name, Label: in.Label}
		if in.Match != nil {
			desc.Match = in.Match.String()
		}
		d.Inputs = append(d.Inputs, desc)
	}
	for _, sh := range e.wf.Sheets {
		d.Sheets = append(d.Sheets, sh.Name)
	}
	return d
}

// describe handles GET /api/<name>.
func (e *endpoint) describe(w http.ResponseWriter, _ *http.Request) error {
	return httpx.WriteJSON(w, http.StatusOK, e.description())
}

// catalog handles GET /api/workflows: every workflow this service runs.
func (m *Module) catalog(w http.ResponseWriter, _ *http.Request) error {
	out := struct {
		Workflows []Description `json:"workflows"`
		Steps     []string      `json:"steps"`
	}{Workflows: []Description{}, Steps: workflow.StepNames()}
	for _, e := range m.endpoints {
		out.Workflows = append(out.Workflows, e.description())
	}
	return httpx.WriteJSON(w, http.StatusOK, out)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// generate runs the workflow into a temp file and commits it atomically, so
// a half-written workbook is never visible or downloadable.
func (e *endpoint) generate(r *http.Request, inputs map[string]string, fileName, owner string) (string, error) {
	if err := e.gate.Acquire(r.Context()); err != nil {
		return "", err
	}
	defer e.gate.Release()

	tmp := e.store.NewTempPath()
	if err := e.wf.Run(r.Context(), e.rt, inputs, tmp); err != nil {
		e.store.Discard(tmp)
		return "", err
	}
	outPath, err := e.store.Commit(tmp, fileName, owner)
	if err != nil {
		e.store.Discard(tmp)
		return "", err
	}
	return outPath, nil
}

// download serves the latest workbook of this workflow.
func (e *endpoint) download(w http.ResponseWriter, r *http.Request) error {
	e.mu.Lock()
	file := e.latest
	e.mu.Unlock()
	if file == "" {
		return httpx.Fail(http.StatusNotFound, "No output file found.")
	}
	return serveWorkbook(w, r, file, filepath.Base(file))
}

func serveWorkbook(w http.ResponseWriter, r *http.Request, file, name string) error {
	f, err := os.Open(file)
	if err != nil {
		return httpx.Fail(http.StatusNotFound, "No output file found.")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	http.ServeContent(w, r, name, st.ModTime(), f)
	return nil
}
