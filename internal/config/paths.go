package config

import (
	"path/filepath"
)

type Paths struct {
	// Nginx
	NginxRoot     string
	NginxBin      string
	NginxMainConf string
	NginxSitesDir string
	NginxStageDir string
	NginxBackupDir string

	// Certs
	CertbotBin      string
	ACMEWebroot     string
	LetsEncryptLive string
}

func (c *Config) ResolvePaths() Paths {
	root := c.Nginx.Root

	return Paths{
		NginxRoot:      root,
		NginxBin:       absOrJoin(root, c.Nginx.Bin),
		NginxMainConf:  absOrJoin(root, c.Nginx.MainConf),
		NginxSitesDir:  absOrJoin(root, c.Nginx.SitesDir),
		NginxStageDir:  absOrJoin(root, c.Nginx.Apply.StagingDir),
		NginxBackupDir: absOrJoin(root, c.Nginx.Apply.BackupDir),

		CertbotBin:      c.Certs.CertbotBin, // can be PATH lookup
		ACMEWebroot:     c.Certs.Webroot,
		LetsEncryptLive: c.Certs.LetsEncryptLive,
	}
}

func absOrJoin(root, p string) string {
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, p)
}
