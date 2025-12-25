package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	API      APIConfig      `yaml:"api"`
	Nginx    NginxConfig    `yaml:"nginx"`
	Certs    CertsConfig    `yaml:"certs"`
	PHPFPM   PHPFPMConfig   `yaml:"phpfpm"`
	Hosting  HostingConfig  `yaml:"hosting"`
	Security SecurityConfig `yaml:"security"`
	Storage  StorageConfig  `yaml:"storage"`
}

type APIConfig struct {
	Listen   string   `yaml:"listen"`
	Tokens   []string `yaml:"tokens"`
	AllowIPs []string `yaml:"allow_ips"`
}

type NginxConfig struct {
	Root     string          `yaml:"root"`
	MainConf string          `yaml:"main_conf"`
	SitesDir string          `yaml:"sites_dir"`
	Bin      string          `yaml:"bin"`
	Apply    NginxApplyConfig `yaml:"apply"`
}

type NginxApplyConfig struct {
	StagingDir       string `yaml:"staging_dir"`
	BackupDir        string `yaml:"backup_dir"`
	TestBeforeReload bool   `yaml:"test_before_reload"`
	ReloadMode       string `yaml:"reload_mode"` // "signal" or "systemd"
}

type CertsConfig struct {
	Mode            string `yaml:"mode"` // "certbot" (MVP)
	Email           string `yaml:"email"`
	Webroot         string `yaml:"webroot"`
	LetsEncryptLive string `yaml:"letsencrypt_live"`
	CertbotBin      string `yaml:"certbot_bin"`
}

type PHPFPMConfig struct {
	DefaultVersion string                    `yaml:"default_version"`
	Versions       map[string]PHPFPMVersion `yaml:"versions"`
}

type PHPFPMVersion struct {
	PoolsDir string `yaml:"pools_dir"`
	Service  string `yaml:"service"`
	SockDir  string `yaml:"sock_dir"`
}

type HostingConfig struct {
	HomeRoot      string `yaml:"home_root"`
	SitesRootName string `yaml:"sites_root_name"`
	WebGroup      string `yaml:"web_group"`
}

type SecurityConfig struct {
	AuditLog string `yaml:"audit_log"`
}

type StorageConfig struct {
	SQLitePath string `yaml:"sqlite_path"`
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true) // catch typos in YAML
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse yaml %q: %w", path, err)
	}

	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	// API
	if c.API.Listen == "" {
		c.API.Listen = "127.0.0.1:9601"
	}

	// Nginx
	if c.Nginx.MainConf == "" {
		c.Nginx.MainConf = "conf/nginx.conf"
	}
	if c.Nginx.SitesDir == "" {
		c.Nginx.SitesDir = "conf/sites"
	}
	if c.Nginx.Bin == "" {
		c.Nginx.Bin = "sbin/nginx"
	}
	if c.Nginx.Apply.StagingDir == "" {
		c.Nginx.Apply.StagingDir = "conf/.staging"
	}
	if c.Nginx.Apply.BackupDir == "" {
		c.Nginx.Apply.BackupDir = "conf/.backup"
	}
	// default true
	if !c.Nginx.Apply.TestBeforeReload {
		c.Nginx.Apply.TestBeforeReload = true
	}
	if c.Nginx.Apply.ReloadMode == "" {
		c.Nginx.Apply.ReloadMode = "signal"
	}

	// Certs
	if c.Certs.Mode == "" {
		c.Certs.Mode = "certbot"
	}
	if c.Certs.CertbotBin == "" {
		c.Certs.CertbotBin = "certbot"
	}

	// PHP-FPM
	if c.PHPFPM.DefaultVersion == "" {
		c.PHPFPM.DefaultVersion = "8.4"
	}
	if c.PHPFPM.Versions == nil {
		c.PHPFPM.Versions = map[string]PHPFPMVersion{}
	}

	// Hosting
	if c.Hosting.HomeRoot == "" {
		c.Hosting.HomeRoot = "/home"
	}
	if c.Hosting.SitesRootName == "" {
		c.Hosting.SitesRootName = "sites"
	}
	if c.Hosting.WebGroup == "" {
		c.Hosting.WebGroup = "www-data"
	}

	// Storage
	if c.Storage.SQLitePath == "" {
		c.Storage.SQLitePath = "/var/lib/ngm/ngm.db"
	}
	// Security
	if c.Security.AuditLog == "" {
		c.Security.AuditLog = "/var/log/ngm/audit.log"
	}
}
