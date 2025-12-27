package nginx

import (
	"fmt"
	"os"
	"path/filepath"

	"mynginx/internal/util"
)

// RemoveLiveSite removes the live vhost file and keeps a backup in BackupDir.
// It does NOT reload. Batch apply will Test+Reload once at the end.
func (m *Manager) RemoveLiveSite(domain string) error {
	dst := filepath.Join(m.SitesDir, domain+".conf")
	bak := filepath.Join(m.BackupDir, domain+".conf.bak")

	// nothing to remove
	if _, err := os.Stat(dst); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat live %s: %w", dst, err)
	}

	// backup existing
	old, err := os.ReadFile(dst)
	if err != nil {
		return fmt.Errorf("read live %s: %w", dst, err)
	}
	if err := util.WriteFileAtomic(bak, old, 0644); err != nil {
		return fmt.Errorf("write backup %s: %w", bak, err)
	}

	// remove live
	if err := os.Remove(dst); err != nil {
		return fmt.Errorf("remove live %s: %w", dst, err)
	}
	return nil
}
