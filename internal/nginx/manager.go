package nginx

import (
	"fmt"
	"os"
	"path/filepath"
)

type Manager struct {
	Root      string
	Bin       string
	MainConf  string
	SitesDir  string
	StageDir  string
	BackupDir string
}

func NewManager(root, bin, mainConf, sitesDir, stageDir, backupDir string) *Manager {
	return &Manager{
		Root:      root,
		Bin:       bin,
		MainConf:  mainConf,
		SitesDir:  sitesDir,
		StageDir:  stageDir,
		BackupDir: backupDir,
	}
}

// EnsureLayout creates the required directories for generated configs.
// It does NOT write configs yet.
func (m *Manager) EnsureLayout() error {
	dirs := []string{
		m.SitesDir,
		m.StageDir,
		m.BackupDir,
	}

	for _, d := range dirs {
		if d == "" {
			continue
		}
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	// Optional: create a dedicated staging sites directory so we can stage safely later
	stageSites := filepath.Join(m.StageDir, "sites")
	if err := os.MkdirAll(stageSites, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", stageSites, err)
	}

	return nil
}
