//go:build darwin || linux || freebsd || openbsd || netbsd

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureGitCancellation(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Kill hooks/SSH along with Git so canceled commands cannot update refs later.
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
