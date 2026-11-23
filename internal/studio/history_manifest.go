package studio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"miservice/internal/workflow"
)

const runManifestFile = "manifest.json"
const runManifestVersion = 1

type runManifest struct {
	Version      int                `json:"version"`
	CompletedAt  string             `json:"completedAt"`
	WorkflowPath string             `json:"workflowPath"`
	WorkflowName string             `json:"workflowName"`
	YAMLSHA256   string             `json:"yamlSha256"`
	Inputs       []runManifestInput `json:"inputs"`
	OutputPath   string             `json:"outputPath"`
	OutputSHA256 string             `json:"outputSha256"`
	Uncertain    bool               `json:"uncertain"`
}

type runManifestInput struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Changed bool   `json:"changed"`
}

// The workflow path identifies the editor document. The run uses the YAML
// received in the request, so the saved file can have moved or been deleted.
func checkWorkflowPath(path string) error {
	if path == "" {
		return nil
	}
	if !fs.ValidPath(path) || path == "." || strings.ContainsAny(path, "\\:\x00") || !filepath.IsLocal(path) {
		return errors.New("Workflow path must be relative to the workspace")
	}
	return nil
}

// Ensure that history lives at the same physical location that the reader scans.
func prepareRunDirectory(root string) (string, error) {
	dir := root
	for _, name := range []string{".studio", "runs"} {
		dir = filepath.Join(dir, name)
		if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
			return "", err
		}
		info, err := os.Lstat(dir)
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", errors.New("Run history path must contain only workspace directories")
		}
	}
	return dir, nil
}

type inputFingerprint struct {
	digest string
	info   os.FileInfo
	stable bool
}

func fingerprintInput(path string) (inputFingerprint, error) {
	f, err := openWorkbookFile(path, maxWorkbookBytes)
	if err != nil {
		return inputFingerprint{}, err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return inputFingerprint{}, err
	}
	if !before.Mode().IsRegular() || before.Size() > maxWorkbookBytes {
		return inputFingerprint{}, errWorkbookFileSize
	}
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(f, maxWorkbookBytes+1))
	if err != nil {
		return inputFingerprint{}, err
	}
	if size > maxWorkbookBytes {
		return inputFingerprint{}, errWorkbookFileSize
	}
	after, err := f.Stat()
	if err != nil {
		return inputFingerprint{}, err
	}
	return inputFingerprint{
		digest: hex.EncodeToString(hash.Sum(nil)),
		info:   before,
		stable: size == before.Size() && sameInputFile(before, after),
	}, nil
}

func sameInputFile(before, after os.FileInfo) bool {
	return os.SameFile(before, after) && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime())
}

func inputChanged(root, binding, originalPath string, before inputFingerprint) bool {
	path, err := localPath(root, binding)
	if !before.stable || err != nil || path != originalPath {
		return true
	}
	after, err := fingerprintInput(path)
	return err != nil || !after.stable || before.digest != after.digest || !sameInputFile(before.info, after.info)
}

func inputBindingLabel(root, workspace, binding, resolved string) (string, error) {
	if !filepath.IsAbs(binding) {
		binding = filepath.Join(root, binding)
	}
	for _, base := range []string{root, workspace} {
		absolute, err := filepath.Abs(base)
		if err != nil {
			continue
		}
		if rel, err := filepath.Rel(absolute, binding); err == nil && fs.ValidPath(filepath.ToSlash(rel)) && rel != "." {
			return filepath.ToSlash(rel), nil
		}
	}
	// A caller can bind an absolute alias outside the workspace that resolves to
	// a file inside it. Record the in-workspace target in that case.
	rel, err := filepath.Rel(root, resolved)
	return filepath.ToSlash(rel), err
}

func persistRun(ctx context.Context, root string, wf *workflow.Workflow, p Params, inputs map[string]string, execute func(string) error) (string, runManifest, error) {
	if err := checkWorkflowPath(p.WorkflowPath); err != nil {
		return "", runManifest{}, err
	}
	dir, err := prepareRunDirectory(root)
	if err != nil {
		return "", runManifest{}, err
	}
	const stagingPrefix = ".pending-"
	stage, err := os.MkdirTemp(dir, stagingPrefix)
	if err != nil {
		return "", runManifest{}, err
	}
	published := false
	defer func() {
		if !published {
			os.RemoveAll(stage)
		}
	}()

	runID := "run-" + strings.TrimPrefix(filepath.Base(stage), stagingPrefix)
	outputName := "output" + wf.Ext()
	stagedOutput := filepath.Join(stage, outputName)
	manifest := runManifest{
		Version:      runManifestVersion,
		WorkflowPath: p.WorkflowPath,
		WorkflowName: wf.Name,
		YAMLSHA256:   fmt.Sprintf("%x", sha256.Sum256([]byte(p.YAML))),
		Inputs:       make([]runManifestInput, 0, len(wf.Inputs)),
		OutputPath:   filepath.ToSlash(filepath.Join(".studio", "runs", runID, outputName)),
	}
	fingerprints := make([]inputFingerprint, 0, len(wf.Inputs))
	for _, input := range wf.Inputs {
		path := inputs[input.Name]
		label, err := inputBindingLabel(root, p.Workspace, p.Inputs[input.Name], path)
		if err != nil {
			return "", runManifest{}, err
		}
		fingerprint, err := fingerprintInput(path)
		if err != nil {
			return "", runManifest{}, fmt.Errorf("Fingerprint input %q: %w", input.Name, err)
		}
		fingerprints = append(fingerprints, fingerprint)
		manifest.Inputs = append(manifest.Inputs, runManifestInput{Name: input.Name, Path: label, SHA256: fingerprint.digest})
	}

	if err := execute(stagedOutput); err != nil {
		return "", runManifest{}, err
	}
	for i := range manifest.Inputs {
		input := &manifest.Inputs[i]
		input.Changed = inputChanged(root, p.Inputs[input.Name], inputs[input.Name], fingerprints[i])
		manifest.Uncertain = manifest.Uncertain || input.Changed
	}
	manifest.OutputSHA256, err = syncOutput(stagedOutput)
	if err != nil {
		return "", runManifest{}, err
	}
	completedAt := time.Now().UTC()
	manifest.CompletedAt = completedAt.Format(time.RFC3339Nano)
	if err := writeRunManifest(stage, manifest); err != nil {
		return "", runManifest{}, err
	}
	if err := os.Chtimes(stage, completedAt, completedAt); err != nil {
		return "", runManifest{}, err
	}
	if err := ctx.Err(); err != nil {
		return "", runManifest{}, err
	}
	// A stage is invisible to the reader until the output and its manifest
	// become visible together. Both files are closed before renaming on Windows.
	final := filepath.Join(dir, runID)
	if _, err := os.Lstat(final); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			err = os.ErrExist
		}
		return "", runManifest{}, err
	}
	if err := os.Rename(stage, final); err != nil {
		return "", runManifest{}, err
	}
	published = true
	return runID, manifest, nil
}

func syncOutput(path string) (string, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, readErr := io.Copy(hash, f)
	syncErr := f.Sync()
	if err := errors.Join(readErr, syncErr, f.Close()); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeRunManifest(dir string, manifest runManifest) error {
	f, err := os.OpenFile(filepath.Join(dir, runManifestFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	writeErr := json.NewEncoder(f).Encode(manifest)
	syncErr := f.Sync()
	return errors.Join(writeErr, syncErr, f.Close())
}
