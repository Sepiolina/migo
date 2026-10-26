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

// upload handles POST /api/<name>/upload: multipart "files" (one per input)
// and "bam" (employee number).
func (e *endpoint) upload(w http.ResponseWriter, r *http.Request) error {
	n := len(e.wf.Inputs)
	up, err := httpx.ParseUpload(r, httpx.UploadOptions{
		Dir: e.uploadDir, FieldName: "files", MaxFiles: n, MaxFileMB: MaxUploadMB,
	})
	if err != nil {
		return err
	}
	defer func() {
		// Uploaded inputs are only needed during this request. Parsing and
		// writing a workbook is the one heavy allocation in this service's
		// life; hand the memory back right away (Node: global.gc()).
		e.uploads.Discard(up.Paths()...) // deletes what is not registered in keep
		jobs.FreeMemory()
	}()

	if len(up.Files) < n {
		labels := make([]string, n)
		for i, in := range e.wf.Inputs {
			labels[i] = in.Label
		}
		return httpx.Fail(http.StatusBadRequest, fmt.Sprintf("Expected %d file%s (%s)", n, plural(n), strings.Join(labels, " & ")))
	}
	emp, ok := employeeNumber(up)
	if !ok {
		return httpx.Fail(http.StatusBadRequest, "Missing or invalid 'bam' (employee number). Use letters, digits, '-' or '_' only.")
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
	fileName := BuildOutputFilename(up.Files[pick[0]].Name, emp)

	outPath, err := e.generate(r, inputs, fileName, emp)
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
		FileName:    fileName,
		DownloadURL: fmt.Sprintf("%s://%s/api/%s/download", scheme, r.Host, e.name),
		Bam:         emp,
	})
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// employeeNumber reads the "bam" field (or the legacy "BOM Number") and
// validates it.
func employeeNumber(up *httpx.Upload) (string, bool) {
	raw := up.Field("bam")
	if raw == "" {
		raw = up.Field("BOM Number")
	}
	return ParseEmployeeNumber(raw)
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
	f, err := os.Open(file)
	if file == "" || err != nil {
		return httpx.Fail(http.StatusNotFound, "No output file found.")
	}
	defer f.Close()
	st, _ := f.Stat()
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(file)}))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	http.ServeContent(w, r, filepath.Base(file), st.ModTime(), f)
	return nil
}
