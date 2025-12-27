package fpm

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func ReloadService(service string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "systemctl", "reload", service)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl reload %s failed: %w (out=%s)", service, err, strings.TrimSpace(string(out)))
	}
	return nil
}
