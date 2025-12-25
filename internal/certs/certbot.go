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
