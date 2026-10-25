package bom

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"

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

// upload handles POST /api/bom/upload: multipart "files" (2: BOM + Priority,
// any order) and "bam" (employee number).
func (m *Module) upload(w http.ResponseWriter, r *http.Request) error {
	up, err := httpx.ParseUpload(r, httpx.UploadOptions{
		Dir: m.uploadDir, FieldName: "files", MaxFiles: uploadFiles, MaxFileMB: MaxUploadMB,
	})
	if err != nil {
		return err
	}
	defer func() {
		// Uploaded inputs are only needed during this request. Parsing and
		// writing a BOM is the one heavy allocation in this service's life;
		// hand the memory back right away (Node: global.gc()).
		m.uploads.Discard(up.Paths()...) // deletes what is not registered in keep
		jobs.FreeMemory()
	}()

	if len(up.Files) < uploadFiles {
		return httpx.Fail(http.StatusBadRequest, "Expected 2 files (Customer BOM & Priority)")
	}
	emp, ok := employeeNumber(up)
	if !ok {
		return httpx.Fail(http.StatusBadRequest, "Missing or invalid 'bam' (employee number). Use letters, digits, '-' or '_' only.")
	}

	bomFile, prioFile := IdentifyUploadedFiles(
		Uploaded{Path: up.Files[0].Path, Name: up.Files[0].Name},
		Uploaded{Path: up.Files[1].Path, Name: up.Files[1].Name},
	)
	fileName := BuildOutputFilename(bomFile.Name, emp)
	owner := emp
	if m.profileID != "" {
		// Separate filenames and retention from both the legacy endpoint and
		// other recipes. The original route keeps its exact filename contract.
		fileName = "pipeline_" + m.profileID + "_" + fileName
		owner = "pipeline:" + m.profileID + ":" + emp
	}

	outPath, err := m.generate(r, bomFile.Path, prioFile.Path, fileName, owner)
	if err != nil {
		return err
	}
	m.setLatest(outPath)

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return httpx.WriteJSON(w, http.StatusOK, successBody{
		Status:      "success",
		Message:     "BOM processed successfully.",
		FileName:    fileName,
		DownloadURL: fmt.Sprintf("%s://%s%s/download", scheme, r.Host, m.routeBase),
		Bam:         emp,
	})
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

// generate runs the pipeline into a temp file and commits it atomically, so
// a half-written workbook is never visible or downloadable.
func (m *Module) generate(r *http.Request, bomPath, prioPath, fileName, emp string) (string, error) {
	if err := m.gate.Acquire(r.Context()); err != nil {
		return "", err
	}
	defer m.gate.Release()

	tmp := m.store.NewTempPath()
	if err := m.pipeline.Process(r.Context(), bomPath, prioPath, tmp); err != nil {
		m.store.Discard(tmp)
		return "", err
	}
	outPath, err := m.store.Commit(tmp, fileName, emp)
	if err != nil {
		m.store.Discard(tmp)
		return "", err
	}
	return outPath, nil
}

func (m *Module) setLatest(path string) {
	m.mu.Lock()
	m.latest = path
	m.mu.Unlock()
}

// download handles /api/bom/download: the latest workbook of this process.
func (m *Module) download(w http.ResponseWriter, r *http.Request) error {
	m.mu.Lock()
	file := m.latest
	m.mu.Unlock()
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
