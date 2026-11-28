package main

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/Sepiolina/migo/engine/cli"
)

// The files generated from the step descriptors are committed; they must
// match what migo writes now.
func TestGeneratedFilesAreCurrent(t *testing.T) {
	for name, path := range map[string]string{
		"describe": "../../apps/studio/src/steps.generated.json",
		"schema":   "../../docs/workflow.schema.json",
		"docs":     "../../docs/STEP_REFERENCE.md",
	} {
		var stdout, stderr bytes.Buffer
		if code := cli.Main(context.Background(), []string{name}, &stdout, &stderr, cli.Options{}); code != 0 {
			t.Fatalf("migo %s: code %d: %s", name, code, &stderr)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), stdout.Bytes()) {
			t.Errorf("%s is out of date: run `make describe` and commit the result", path)
		}
	}
}
