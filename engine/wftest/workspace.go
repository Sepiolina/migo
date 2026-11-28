package wftest

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// WorkspacePath resolves a reference against base and checks containment after
// following symlinks. A missing path is allowed when its parent is contained.
func WorkspacePath(root, base, path string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	target, err := absolutePath(base, path)
	if err != nil {
		return "", err
	}
	if !inside(root, target) && !inside(canonicalRoot, target) {
		return "", fmt.Errorf("%q is outside the workspace", path)
	}
	canonicalPath, err := resolveMissing(target)
	if err != nil {
		return "", err
	}
	if !inside(canonicalRoot, canonicalPath) {
		return "", fmt.Errorf("%q is outside the workspace", path)
	}
	return canonicalPath, nil
}

func absolutePath(base, path string) (string, error) {
	path = filepath.FromSlash(path)
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	return filepath.Abs(path)
}

func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && filepath.IsLocal(rel)
}

func resolveMissing(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if !errors.Is(err, os.ErrNotExist) {
		return resolved, err
	}
	if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	resolved, err = resolveMissing(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(path)), nil
}

func workspaceFile(root, base, path string) (string, error) {
	resolved, err := WorkspacePath(root, base, path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if errors.Is(err, os.ErrNotExist) {
		return resolved, nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%q must be a regular file", path)
	}
	return resolved, nil
}

// LoadWorkspace opens a suite inside root and retains its lexical path as the
// base for relative references.
func LoadWorkspace(root, path string) (*File, error) {
	original, err := absolutePath(root, path)
	if err != nil {
		return nil, err
	}
	resolved, err := workspaceFile(root, root, original)
	if err != nil {
		return nil, err
	}
	const maxSuiteBytes = 1 << 20
	file, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxSuiteBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSuiteBytes {
		return nil, fmt.Errorf("%q must be at most 1 MiB", path)
	}
	return parse(original, data)
}

// WorkflowPath returns the workspace-contained workflow referenced by the suite.
func (f *File) WorkflowPath(root string) (string, error) {
	return f.workspaceResolver(root)(f.Workflow)
}

func (f *File) workspaceResolver(root string) resolver {
	return func(path string) (string, error) {
		if strings.TrimSpace(path) == "" {
			return "", errors.New("empty file path")
		}
		suite, err := absolutePath(root, f.Path)
		if err != nil {
			return "", err
		}
		if _, err := workspaceFile(root, root, suite); err != nil {
			return "", err
		}
		return workspaceFile(root, filepath.Dir(suite), path)
	}
}
