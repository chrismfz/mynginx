package nginx

import (
	"fmt"
	"time"

	"mynginx/internal/util"
)

func (m *Manager) TestConfig() error {
	// Use -c explicitly to avoid relying on cwd/defaults.
	res, err := util.Run(10*time.Second, m.Bin, "-t", "-c", m.MainConf)

	// nginx prints most diagnostics on stderr even on success
	if res.Stdout != "" {
		fmt.Print(res.Stdout)
	}
	if res.Stderr != "" {
		fmt.Print(res.Stderr)
	}

	if err != nil {
		return err
	}
	return nil
}
