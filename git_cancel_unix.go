//go:build darwin || linux || freebsd || openbsd || netbsd

package main

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

func configureGitCancellation(cmd *exec.Cmd) func() {
	var mu sync.Mutex
	var escalation *time.Timer
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Give Git and hooks a chance to release lockfiles before escalating.
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		if err == nil {
			mu.Lock()
			escalation = time.AfterFunc(2*time.Second, func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
			mu.Unlock()
		}
		return err
	}
	return func() {
		mu.Lock()
		defer mu.Unlock()
		if escalation != nil {
			escalation.Stop()
		}
	}
}
