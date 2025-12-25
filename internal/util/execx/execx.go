package execx

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func Run(timeout time.Duration, name string, args ...string) (Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)

	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb

	err := cmd.Run()

	res := Result{
		Stdout: outb.String(),
		Stderr: errb.String(),
	}

	// Timeout?
	if ctx.Err() == context.DeadlineExceeded {
		res.ExitCode = -1
		return res, fmt.Errorf("command timeout after %s: %s %v", timeout, name, args)
	}

	if err == nil {
		res.ExitCode = 0
		return res, nil
	}

	// Non-zero exit
	if ee, ok := err.(*exec.ExitError); ok {
		res.ExitCode = ee.ExitCode()
		return res, fmt.Errorf("command failed (exit %d): %s %v", res.ExitCode, name, args)
	}

	return res, fmt.Errorf("command error: %s %v: %w", name, args, err)
}
