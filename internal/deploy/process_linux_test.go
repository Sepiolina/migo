//go:build linux

package deploy

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestDesktopBuildProcessCleanup(t *testing.T) {
	if os.Getenv("MAI_TEST_PROCESS_HELPER") == "1" {
		// The real runner handles SIGTERM with signal.NotifyContext.
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM)
		defer signal.Stop(signals)
		ctx, cancel := context.WithCancel(context.Background())
		cmd := exec.CommandContext(ctx, "sh", "-c", `(sleep 1; touch "$MAI_TEST_MARKER") & wait`)
		configureBuildProcess(cmd)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
		cancel()
		_ = cmd.Wait()
		return
	}

	marker := filepath.Join(t.TempDir(), "survived")
	cmd := exec.Command(os.Args[0], "-test.run=^TestDesktopBuildProcessCleanup$")
	cmd.Env = append(os.Environ(), "MAI_TEST_PROCESS_HELPER=1", "MAI_RUNNER_PROCESS_GROUP=1", "MAI_TEST_MARKER="+marker)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helper failed: %v: %s", err, output)
	}
	time.Sleep(1200 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("build descendant survived cancellation: %v", err)
	}
}
