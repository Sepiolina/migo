//go:build linux

package deploy

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// The Go command can leave compiler children behind if only its own process is killed.
func configureBuildProcess(cmd *exec.Cmd) {
	// The desktop runner owns its process group. Keeping the build in that
	// group lets the native host terminate the entire tree when the request ends.
	if os.Getenv("MAI_RUNNER_PROCESS_GROUP") == "1" && syscall.Getpgrp() == os.Getpid() {
		// SIGTERM also reaches the runner, which handles it as context
		// cancellation and can return the build error before the host cleans up.
		cmd.Cancel = func() error { return syscall.Kill(-syscall.Getpgrp(), syscall.SIGTERM) }
		cmd.WaitDelay = 5 * time.Second
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
}
