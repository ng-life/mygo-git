//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd

package main

import "os/exec"

func configureGitCancellation(cmd *exec.Cmd) {}
