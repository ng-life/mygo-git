package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Separate stderr from machine-readable stdout and bound pipe waits after cancellation.
func commandGit(parent context.Context, timeout time.Duration, path string, args ...string) ([]byte, error) {
	return commandGitEnv(parent, timeout, path, nil, args...)
}

func commandGitEnv(parent context.Context, timeout time.Duration, path string, overrides map[string]string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", path}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true", "GIT_MERGE_AUTOEDIT=no", "GIT_PAGER=cat", "GIT_SSH_COMMAND=ssh -oBatchMode=yes -oConnectTimeout=15")
	for key, value := range overrides {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	configureGitCancellation(cmd)
	cmd.WaitDelay = 250 * time.Millisecond
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	if err != nil {
		return out, fmt.Errorf("%s: %w", stderr.String(), err)
	}
	return out, nil
}
