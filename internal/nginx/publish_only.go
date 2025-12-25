package nginx

import (
	"fmt"
	"os"
	"path/filepath"

	"mynginx/internal/util/atomic"
)

func (m *Manager) PublishOnly(domain string) error {
	src := filepath.Join(m.StageDir, "sites", domain+".conf")
	dst := filepath.Join(m.SitesDir, domain+".conf")
	bak := filepath.Join(m.BackupDir, domain+".conf.bak")

	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read staging %s: %w", src, err)
	}

	// backup existing live
	if _, err := os.Stat(dst); err == nil {
		old, err := os.ReadFile(dst)
		if err != nil {
			return fmt.Errorf("read live %s: %w", dst, err)
		}
		if err := atomic.WriteFileAtomic(bak, old, 0644); err != nil {
			return fmt.Errorf("write backup %s: %w", bak, err)
		}
	}

	return atomic.WriteFileAtomic(dst, data, 0644)
}
