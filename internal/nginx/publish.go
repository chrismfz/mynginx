package nginx

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
	"bytes"
	"mynginx/internal/util"
)

// Publish copies a staged site config into the live sites directory.
// It creates/updates a backup file when the live file exists.
// It returns changed=false if the live file already matches the staged content.
func (m *Manager) Publish(domain string) (bool, error) {
	if domain == "" {
		return false, fmt.Errorf("domain is required")
	}

	src := filepath.Join(m.StageDir, "sites", domain+".conf")
	dst := filepath.Join(m.SitesDir, domain+".conf")
	bak := filepath.Join(m.BackupDir, domain+".conf.bak")

	data, err := os.ReadFile(src)
	if err != nil {
		return false, fmt.Errorf("read staging %s: %w", src, err)
	}


	// If live exists and content is identical, skip publish.
	if live, err := os.ReadFile(dst); err == nil {
		if bytes.Equal(live, data) {
			return false, nil
		}
	}


	// Backup current live file (if exists)
	if _, err := os.Stat(dst); err == nil {
		old, err := os.ReadFile(dst)
		if err != nil {
			return false, fmt.Errorf("read live %s: %w", dst, err)
		}
		if err := util.WriteFileAtomic(bak, old, 0644); err != nil {
			return false, fmt.Errorf("write backup %s: %w", bak, err)
		}
	}

	// Publish new file atomically
	if err := util.WriteFileAtomic(dst, data, 0644); err != nil {
		return false, fmt.Errorf("publish %s: %w", dst, err)
	}


	return true, nil
}

func (m *Manager) Reload() error {
	// MVP: only "signal" for now; we can add systemd mode later using cfg.Nginx.Apply.ReloadMode
	res, err := util.Run(10*time.Second, m.Bin, "-s", "reload")
	if res.Stdout != "" {
		fmt.Print(res.Stdout)
	}
	if res.Stderr != "" {
		fmt.Print(res.Stderr)
	}
	return err
}
