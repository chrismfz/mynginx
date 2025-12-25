package config

import (
	"fmt"
	"net"
	"strings"
)

func (c *Config) Validate() error {
	var errs []string

	// Nginx basics
	if strings.TrimSpace(c.Nginx.Root) == "" {
		errs = append(errs, "nginx.root is required (e.g. /opt/nginx)")
	}

	// API auth basics
	if len(c.API.Tokens) == 0 {
		errs = append(errs, "api.tokens must contain at least one token")
	}
	for i, t := range c.API.Tokens {
		if strings.TrimSpace(t) == "" {
			errs = append(errs, fmt.Sprintf("api.tokens[%d] is empty", i))
		}
	}

	// Allowlist CIDRs (optional but recommended)
	for i, cidr := range c.API.AllowIPs {
		if strings.TrimSpace(cidr) == "" {
			errs = append(errs, fmt.Sprintf("api.allow_ips[%d] is empty", i))
			continue
		}
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			errs = append(errs, fmt.Sprintf("api.allow_ips[%d]=%q invalid CIDR: %v", i, cidr, err))
		}
	}

	// Certs
	if c.Certs.Mode != "" && c.Certs.Mode != "certbot" {
		errs = append(errs, fmt.Sprintf("certs.mode=%q unsupported (MVP supports only 'certbot')", c.Certs.Mode))
	}
	if strings.TrimSpace(c.Certs.Webroot) == "" {
		errs = append(errs, "certs.webroot is required (e.g. /opt/nginx/html)")
	}
	if strings.TrimSpace(c.Certs.LetsEncryptLive) == "" {
		errs = append(errs, "certs.letsencrypt_live is required (e.g. /etc/letsencrypt/live)")
	}

	// PHP versions map (optional, but if present must be consistent)
	if c.PHPFPM.DefaultVersion != "" {
		if _, ok := c.PHPFPM.Versions[c.PHPFPM.DefaultVersion]; !ok && len(c.PHPFPM.Versions) > 0 {
			errs = append(errs, fmt.Sprintf("phpfpm.default_version=%q not found in phpfpm.versions map", c.PHPFPM.DefaultVersion))
		}
	}
	for ver, v := range c.PHPFPM.Versions {
		if strings.TrimSpace(v.PoolsDir) == "" {
			errs = append(errs, fmt.Sprintf("phpfpm.versions[%q].pools_dir is required", ver))
		}
		if strings.TrimSpace(v.Service) == "" {
			errs = append(errs, fmt.Sprintf("phpfpm.versions[%q].service is required", ver))
		}
		if strings.TrimSpace(v.SockDir) == "" {
			errs = append(errs, fmt.Sprintf("phpfpm.versions[%q].sock_dir is required", ver))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation failed:\n- %s", strings.Join(errs, "\n- "))
	}
	return nil
}
