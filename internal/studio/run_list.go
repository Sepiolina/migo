package studio

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const defaultRunListLimit = 20
const maxRunListLimit = 100

var runOutputNames = [...]string{"output.xlsx", "output.csv"}

type runEntry struct {
	ID          string       `json:"id"`
	OutputPath  string       `json:"outputPath"`
	Available   bool         `json:"available"`
	CompletedAt string       `json:"completedAt"`
	Manifest    *runManifest `json:"manifest,omitempty"`
}

type runPage struct {
	Runs       []runEntry `json:"runs"`
	NextOffset int        `json:"nextOffset,omitempty"`
}

func listRuns(ctx context.Context, req Request) (runPage, error) {
	page := runPage{Runs: []runEntry{}}
	p := req.Params
	if p.Workspace == "" {
		return page, errors.New("A workspace is required to list runs")
	}
	if p.Offset < 0 || p.Limit < 0 || p.Limit > maxRunListLimit {
		return page, errors.New("Offset must be nonnegative and limit at most 100")
	}
	limit := p.Limit
	if limit == 0 {
		limit = defaultRunListLimit
	}
	root, err := localPath(p.Workspace, ".")
	if err != nil {
		return page, err
	}
	history := root
	for _, name := range []string{".studio", "runs"} {
		history = filepath.Join(history, name)
		info, err := os.Lstat(history)
		if errors.Is(err, fs.ErrNotExist) {
			return page, nil
		}
		if err != nil {
			return page, err
		}
		if !info.IsDir() {
			return page, errors.New("Run history must use directories inside the workspace")
		}
	}
	dirs, err := os.ReadDir(history)
	if err != nil {
		return page, err
	}
	var runs []fs.FileInfo
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return page, err
		}
		if !dir.IsDir() || !strings.HasPrefix(dir.Name(), "run-") || dir.Name() == "run-" {
			continue
		}
		if info, err := dir.Info(); err == nil && info.IsDir() {
			runs = append(runs, info)
		}
	}
	// Directory timestamps support ordering without opening manifests across history.
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].ModTime().Equal(runs[j].ModTime()) {
			return runs[i].Name() > runs[j].Name()
		}
		return runs[i].ModTime().After(runs[j].ModTime())
	})
	if p.Offset >= len(runs) {
		return page, nil
	}

	// Reserve the response envelope, including the serialized ID, pagination and newline.
	envelope, err := json.Marshal(Response{
		Version: Version, ID: req.ID, OK: true,
		Result: runPage{Runs: []runEntry{}, NextOffset: len(runs)},
	})
	if err != nil {
		return page, err
	}
	budget := MaxFrameBytes - len(envelope) - 2
	end := p.Offset + min(limit, len(runs)-p.Offset)
	for i := p.Offset; i < end; i++ {
		if err := ctx.Err(); err != nil {
			return page, err
		}
		entry := readRunEntry(root, history, runs[i])
		encoded, err := json.Marshal(entry)
		if err != nil {
			return page, err
		}
		// A single oversized manifest degrades to an entry with basic output details.
		if len(encoded) > budget && len(page.Runs) == 0 {
			entry.Manifest = nil
			encoded, err = json.Marshal(entry)
			if err != nil {
				return page, err
			}
		}
		if len(encoded) > budget {
			if len(page.Runs) == 0 {
				return page, errRunPageTooLarge
			}
			break
		}
		page.Runs = append(page.Runs, entry)
		budget -= len(encoded) + 1
	}
	if next := p.Offset + len(page.Runs); next < len(runs) {
		page.NextOffset = next
	}
	return page, nil
}

var errRunPageTooLarge = errors.New("Run history response exceeds 1 MiB")

func readRunEntry(root, history string, info fs.FileInfo) runEntry {
	entry := runEntry{ID: info.Name(), CompletedAt: info.ModTime().UTC().Format(time.RFC3339Nano)}
	dir := filepath.Join(history, entry.ID)
	relDir := filepath.ToSlash(filepath.Join(".studio", "runs", entry.ID))
	if !isRunDirectory(root, dir, info) {
		return entry
	}
	entry.Manifest = readListedManifest(root, dir, relDir, info)
	if manifest := entry.Manifest; manifest != nil {
		entry.CompletedAt = manifest.CompletedAt
		entry.OutputPath = manifest.OutputPath
		entry.Available = listedOutputAvailable(root, dir, filepath.Base(manifest.OutputPath), info)
		return entry
	}
	for _, name := range runOutputNames {
		if listedOutputAvailable(root, dir, name, info) {
			entry.OutputPath = relDir + "/" + name
			entry.Available = true
			break
		}
	}
	return entry
}

func isRunDirectory(root, dir string, original fs.FileInfo) bool {
	resolved, err := localPath(root, dir)
	if err != nil || resolved != dir {
		return false
	}
	info, err := os.Lstat(dir)
	return err == nil && info.IsDir() && os.SameFile(original, info)
}

func listedOutputAvailable(root, dir, name string, original fs.FileInfo) bool {
	info, err := os.Lstat(filepath.Join(dir, name))
	return err == nil && info.Mode().IsRegular() && isRunDirectory(root, dir, original)
}

func readListedManifest(root, dir, relDir string, original fs.FileInfo) *runManifest {
	path := filepath.Join(dir, runManifestFile)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxFrameBytes {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(opened, info) || !isRunDirectory(root, dir, original) {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxFrameBytes+1))
	if err != nil || len(data) > MaxFrameBytes {
		return nil
	}
	var manifest runManifest
	if json.Unmarshal(data, &manifest) != nil || !validListedManifest(manifest, relDir) {
		return nil
	}
	return &manifest
}

func validListedManifest(manifest runManifest, relDir string) bool {
	if manifest.Version != runManifestVersion || manifest.Inputs == nil || !validRunDigest(manifest.YAMLSHA256) || !validRunDigest(manifest.OutputSHA256) {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, manifest.CompletedAt); err != nil {
		return false
	}
	if checkWorkflowPath(manifest.WorkflowPath) != nil {
		return false
	}
	for _, input := range manifest.Inputs {
		if input.Name == "" || input.Path == "." || !fs.ValidPath(input.Path) || !filepath.IsLocal(input.Path) || strings.ContainsRune(input.Path, 0) || !validRunDigest(input.SHA256) {
			return false
		}
	}
	for _, name := range runOutputNames {
		if manifest.OutputPath == relDir+"/"+name {
			return true
		}
	}
	return false
}

func validRunDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
