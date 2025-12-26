package certs

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"sort"
)

type CertbotManager struct {
	CertbotBin      string // "certbot" or full path
	Webroot         string // /opt/nginx/html
	LetsEncryptLive string // /etc/letsencrypt/live
	Email           string // admin@example.com
}

// CertInfo holds certificate information
type CertInfo struct {
	Domain    string
	CertPath  string
	KeyPath   string
	NotBefore time.Time
	NotAfter  time.Time
	DaysLeft  int
	Exists    bool
}


func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func isSymlink(fi os.FileInfo) bool {
	return fi.Mode()&os.ModeSymlink != 0
}

// ensureLiveAlias ensures /live/<domain> exists (as real dir or symlink) pointing to a valid
// certbot live directory (e.g. /live/<domain>-0001) that contains fullchain.pem + privkey.pem.
//
// It returns the resolved directory path that should contain fullchain.pem.
func (m *CertbotManager) ensureLiveAlias(domain string) (string, error) {
	if domain == "" {
		return "", fmt.Errorf("domain is required")
	}

	baseDir := filepath.Join(m.LetsEncryptLive, domain)
	baseFull := filepath.Join(baseDir, "fullchain.pem")
	baseKey := filepath.Join(baseDir, "privkey.pem")

	// Already OK.
	if fileExists(baseFull) && fileExists(baseKey) {
		return baseDir, nil
	}

	// Find candidates: /live/<domain>-*
	glob := filepath.Join(m.LetsEncryptLive, domain+"-*")
	cands, _ := filepath.Glob(glob)

	type cand struct {
		dir      string
		notAfter time.Time
		modTime  time.Time
	}
	var good []cand

	for _, d := range cands {
		full := filepath.Join(d, "fullchain.pem")
		key := filepath.Join(d, "privkey.pem")
		if !fileExists(full) || !fileExists(key) {
			continue
		}

		// Try to parse cert expiry as a “quality” signal.
		na := time.Time{}
		if info, err := m.getCertInfoFromPath(domain, full, key); err == nil && info.Exists {
			na = info.NotAfter
		}

		// ModTime fallback (directory mtime is unreliable on some FS, but OK as a tiebreaker)
		mt := time.Time{}
		if st, err := os.Stat(d); err == nil {
			mt = st.ModTime()
		}

		good = append(good, cand{dir: d, notAfter: na, modTime: mt})
	}

	if len(good) == 0 {
		// Nothing we can alias to.
		return baseDir, nil
	}

	// Pick “best” candidate:
	// 1) latest NotAfter (if available)
	// 2) latest ModTime
	sort.Slice(good, func(i, j int) bool {
		ai, aj := good[i].notAfter, good[j].notAfter
		if !ai.IsZero() && !aj.IsZero() && !ai.Equal(aj) {
			return ai.After(aj)
		}
		if !ai.IsZero() && aj.IsZero() {
			return true
		}
		if ai.IsZero() && !aj.IsZero() {
			return false
		}
		return good[i].modTime.After(good[j].modTime)
	})
	target := good[0].dir

	// Ensure baseDir is a symlink to target (or rename existing real dir if needed).
	if st, err := os.Lstat(baseDir); err == nil {
		if isSymlink(st) {
			// Remove and recreate to be sure.
			_ = os.Remove(baseDir)
		} else if st.IsDir() {
			// Safety: don't delete. Rename it out of the way and then create alias.
			backup := baseDir + ".bak-" + time.Now().Format("20060102-150405")
			if err := os.Rename(baseDir, backup); err != nil {
				return "", fmt.Errorf("cannot rename existing live dir %s (to %s): %w", baseDir, backup, err)
			}
		} else {
			// File? Unexpected.
			backup := baseDir + ".bak-" + time.Now().Format("20060102-150405")
			if err := os.Rename(baseDir, backup); err != nil {
				return "", fmt.Errorf("cannot rename existing live path %s (to %s): %w", baseDir, backup, err)
			}
		}
	}

	// Create symlink: baseDir -> target
	if err := os.Symlink(target, baseDir); err != nil {
		return "", fmt.Errorf("create symlink %s -> %s failed: %w", baseDir, target, err)
	}

	return baseDir, nil
}



// getCertInfoFromPath parses a cert/key pair at explicit paths (used for probing candidates).
func (m *CertbotManager) getCertInfoFromPath(domain, certPath, keyPath string) (*CertInfo, error) {
	info := &CertInfo{
		Domain:   domain,
		CertPath: certPath,
		KeyPath:  keyPath,
		Exists:   false,
	}

	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		return info, nil
	}
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		return info, nil
	}
	info.Exists = true

	certData, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("read cert file: %w", err)
	}
	block, _ := pem.Decode(certData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}

	info.NotBefore = cert.NotBefore
	info.NotAfter = cert.NotAfter
	info.DaysLeft = int(time.Until(cert.NotAfter).Hours() / 24)
	return info, nil
}


// NewCertbotManager creates a new certbot manager
func NewCertbotManager(certbotBin, webroot, letsEncryptLive, email string) *CertbotManager {
	return &CertbotManager{
		CertbotBin:      certbotBin,
		Webroot:         webroot,
		LetsEncryptLive: letsEncryptLive,
		Email:           email,
	}
}

// IssueCert issues a new certificate for the domain using HTTP-01 challenge
// It ensures the webroot exists before attempting issuance
func (m *CertbotManager) IssueCert(ctx context.Context, domain string) error {
	if domain == "" {
		return fmt.Errorf("domain is required")
	}

	// Ensure webroot exists and is accessible
	if err := os.MkdirAll(m.Webroot, 0755); err != nil {
		return fmt.Errorf("create webroot: %w", err)
	}

	// Check if cert already exists
	info, err := m.GetCertInfo(domain)
	if err == nil && info.Exists {
		// Cert exists - check if it's valid
		if info.DaysLeft > 30 {
			return fmt.Errorf("certificate already exists and is valid for %d more days", info.DaysLeft)
		}
		// If less than 30 days, allow renewal
	}

	args := []string{
		"certonly",
		"--webroot",
		"-w", m.Webroot,
		"-d", domain,
		"--cert-name", domain,
		"--non-interactive",
		"--agree-tos",
		"--keep-until-expiring", // Don't re-issue if cert is still valid
	}

	if m.Email != "" {
		args = append(args, "--email", m.Email)
	} else {
		args = append(args, "--register-unsafely-without-email")
	}

	cmd := exec.CommandContext(ctx, m.CertbotBin, args...)
	out, err := cmd.CombinedOutput()
	
	if err != nil {
		return fmt.Errorf("certbot failed: %w\nOutput: %s", err, string(out))
	}

	// If certbot created a suffixed lineage (domain-0001), fix it by creating
	// /live/<domain> alias so the rest of the system can always use /live/<domain>/...
	if _, err := m.ensureLiveAlias(domain); err != nil {
		return fmt.Errorf("cert issued but failed to ensure live alias: %w", err)
	}

	// Verify the cert was actually created
	certPath := filepath.Join(m.LetsEncryptLive, domain, "fullchain.pem")
	if _, err := os.Stat(certPath); err != nil {
		return fmt.Errorf("cert file not found after issuance: %w", err)
	}

	return nil
}

// RenewCert attempts to renew a certificate
func (m *CertbotManager) RenewCert(ctx context.Context, domain string) error {
	if domain == "" {
		return fmt.Errorf("domain is required")
	}

	args := []string{
		"renew",
		"--cert-name", domain,
		"--webroot",
		"-w", m.Webroot,
		"--non-interactive",
	}

	cmd := exec.CommandContext(ctx, m.CertbotBin, args...)
	out, err := cmd.CombinedOutput()
	
	if err != nil {
		return fmt.Errorf("certbot renew failed: %w\nOutput: %s", err, string(out))
	}

	return nil
}

// RenewAll attempts to renew all certificates
func (m *CertbotManager) RenewAll(ctx context.Context) error {
	args := []string{
		"renew",
		"--webroot",
		"-w", m.Webroot,
		"--non-interactive",
	}

	cmd := exec.CommandContext(ctx, m.CertbotBin, args...)
	out, err := cmd.CombinedOutput()
	
	if err != nil {
		return fmt.Errorf("certbot renew all failed: %w\nOutput: %s", err, string(out))
	}

	return nil
}

// GetCertInfo retrieves information about a certificate
func (m *CertbotManager) GetCertInfo(domain string) (*CertInfo, error) {
	// If certbot created domain-0001 etc, make sure /live/<domain> points to it.
	_, _ = m.ensureLiveAlias(domain)

	certPath := filepath.Join(m.LetsEncryptLive, domain, "fullchain.pem")
	keyPath := filepath.Join(m.LetsEncryptLive, domain, "privkey.pem")

	info := &CertInfo{
		Domain:   domain,
		CertPath: certPath,
		KeyPath:  keyPath,
		Exists:   false,
	}

	// Check if cert files exist
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		return info, nil // Not an error, just doesn't exist
	}
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		return info, nil
	}

	info.Exists = true

	// Parse certificate to get expiry info
	certData, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("read cert file: %w", err)
	}

	block, _ := pem.Decode(certData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}

	info.NotBefore = cert.NotBefore
	info.NotAfter = cert.NotAfter
	info.DaysLeft = int(time.Until(cert.NotAfter).Hours() / 24)

	return info, nil
}

// ListCerts returns information about all certificates in the live directory
func (m *CertbotManager) ListCerts() ([]*CertInfo, error) {
	entries, err := os.ReadDir(m.LetsEncryptLive)
	if err != nil {
		if os.IsNotExist(err) {
			return []*CertInfo{}, nil
		}
		return nil, fmt.Errorf("read letsencrypt live dir: %w", err)
	}

	var certs []*CertInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		
		// Skip README if it exists
		if entry.Name() == "README" {
			continue
		}

		info, err := m.GetCertInfo(entry.Name())
		if err != nil {
			// Log but don't fail the entire listing
			continue
		}

		if info.Exists {
			certs = append(certs, info)
		}
	}

	return certs, nil
}

// CheckExpiringSoon returns certificates that expire within the given number of days
func (m *CertbotManager) CheckExpiringSoon(days int) ([]*CertInfo, error) {
	allCerts, err := m.ListCerts()
	if err != nil {
		return nil, err
	}

	var expiring []*CertInfo
	for _, cert := range allCerts {
		if cert.DaysLeft <= days {
			expiring = append(expiring, cert)
		}
	}

	return expiring, nil
}

// EnsureCertExists checks if a cert exists and issues it if not
// This is safe to call even if cert already exists
func (m *CertbotManager) EnsureCertExists(ctx context.Context, domain string) error {
	info, err := m.GetCertInfo(domain)
	if err != nil {
		return fmt.Errorf("check cert info: %w", err)
	}

	if info.Exists && info.DaysLeft > 30 {
		// Cert exists and is valid
		return nil
	}

	// Need to issue or renew
	return m.IssueCert(ctx, domain)
}

// DeleteCert removes certificate files for a domain (e.g., when removing a site)
func (m *CertbotManager) DeleteCert(ctx context.Context, domain string) error {
	if domain == "" {
		return fmt.Errorf("domain is required")
	}

	args := []string{
		"delete",
		"--cert-name", domain,
		"--non-interactive",
	}

	cmd := exec.CommandContext(ctx, m.CertbotBin, args...)
	out, err := cmd.CombinedOutput()
	
	if err != nil {
		// Check if error is because cert doesn't exist (not a real error)
		if strings.Contains(string(out), "No certificate found") {
			return nil
		}
		return fmt.Errorf("certbot delete failed: %w\nOutput: %s", err, string(out))
	}

	return nil
}

// RevokeCert revokes a certificate (useful before deleting domain)
func (m *CertbotManager) RevokeCert(ctx context.Context, domain string) error {
	if domain == "" {
		return fmt.Errorf("domain is required")
	}

	certPath := filepath.Join(m.LetsEncryptLive, domain, "fullchain.pem")
	
	args := []string{
		"revoke",
		"--cert-path", certPath,
		"--non-interactive",
	}

	cmd := exec.CommandContext(ctx, m.CertbotBin, args...)
	out, err := cmd.CombinedOutput()
	
	if err != nil {
		return fmt.Errorf("certbot revoke failed: %w\nOutput: %s", err, string(out))
	}

	return nil
}
