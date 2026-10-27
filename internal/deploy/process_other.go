//go:build !linux

package deploy

import "os/exec"

func configureBuildProcess(cmd *exec.Cmd) {}
