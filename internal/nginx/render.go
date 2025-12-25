package nginx

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"mynginx/internal/util/atomic"
)

func (m *Manager) RenderSiteToStaging(site SiteTemplateData) (string, []byte, error) {
	if site.Domain == "" {
		return "", nil, fmt.Errorf("site.Domain is required")
	}
	if site.Mode == "" {
		site.Mode = "php"
	}
	if site.ACMEWebroot == "" {
		return "", nil, fmt.Errorf("site.ACMEWebroot is required")
	}
	if site.Webroot == "" {
		return "", nil, fmt.Errorf("site.Webroot is required")
	}
	if site.TLSCert == "" || site.TLSKey == "" {
		return "", nil, fmt.Errorf("site TLSCert/TLSKey are required")
	}

	site.UpstreamKey = MakeUpstreamKey(site.Domain)

	tplPath := filepath.Join("internal", "nginx", "templates", "site.tmpl")
	tpl, err := template.ParseFiles(tplPath)
	if err != nil {
		return "", nil, fmt.Errorf("parse template %s: %w", tplPath, err)
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, site); err != nil {
		return "", nil, fmt.Errorf("execute template: %w", err)
	}

	outDir := filepath.Join(m.StageDir, "sites")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", nil, fmt.Errorf("mkdir %s: %w", outDir, err)
	}

	outPath := filepath.Join(outDir, site.Domain+".conf")
	if err := atomic.WriteFileAtomic(outPath, buf.Bytes(), 0644); err != nil {
		return "", nil, err
	}
	return outPath, buf.Bytes(), nil
}
