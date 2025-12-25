package nginx

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"mynginx/internal/util/atomic"
	"mynginx/internal/util/execx"
)

func (m *Manager) PublishSiteFromStaging(domain string) (string, error) {
	if domain == "" {
		return "", fmt.Errorf("domain is required")
	}

	src := filepath.Join(m.StageDir, "sites", domain+".conf")
	dst := filepath.Join(m.SitesDir, domain+".conf")
	bak := filepath.Join(m.BackupDir, domain+".conf.bak")

	data, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("read staging %s: %w", src, err)
	}

	// Backup current live file (if exists)
	if _, err := os.Stat(dst); err == nil {
		old, err := os.ReadFile(dst)
		if err != nil {
			return "", fmt.Errorf("read live %s: %w", dst, err)
		}
		if err := atomic.WriteFileAtomic(bak, old, 0644); err != nil {
			return "", fmt.Errorf("write backup %s: %w", bak, err)
		}
	}

	// Publish new file atomically
	if err := atomic.WriteFileAtomic(dst, data, 0644); err != nil {
		return "", fmt.Errorf("publish %s: %w", dst, err)
	}

	// Test full nginx config (important: this validates includes + new site)
	if err := m.TestConfig(); err != nil {
		// rollback
		_ = m.rollbackSite(dst, bak)
		return "", fmt.Errorf("nginx -t failed after publish; rolled back: %w", err)
	}

	// Reload nginx
	if err := m.Reload(); err != nil {
		_ = m.rollbackSite(dst, bak)
		return "", fmt.Errorf("nginx reload failed; rolled back: %w", err)
	}

	return dst, nil
}

func (m *Manager) rollbackSite(dst, bak string) error {
	// If backup exists, restore it; otherwise remove dst.
	if _, err := os.Stat(bak); err == nil {
		data, err := os.ReadFile(bak)
		if err != nil {
			return err
		}
		return atomic.WriteFileAtomic(dst, data, 0644)
	}
	_ = os.Remove(dst)
	return nil
}

func (m *Manager) Reload() error {
	// MVP: only "signal" for now; we can add systemd mode later using cfg.Nginx.Apply.ReloadMode
	res, err := execx.Run(10*time.Second, m.Bin, "-s", "reload")
	if res.Stdout != "" {
		fmt.Print(res.Stdout)
	}
	if res.Stderr != "" {
		fmt.Print(res.Stderr)
	}
	return err
}
