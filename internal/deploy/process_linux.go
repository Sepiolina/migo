//go:build linux

package deploy

import (
	"os/exec"
	"syscall"
	"time"
)

// The Go command can leave compiler children behind if only its own process is killed.
func configureBuildProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
}
