package fpm

import (
	"bytes"
	"fmt"
	"path/filepath"
	"text/template"

	"mynginx/internal/util"
)

type PoolData struct {
	PoolName    string
	RunUser     string
	RunGroup    string
	Socket      string
	ListenOwner string
	ListenGroup string

	MaxChildren int
	IdleTimeout string
	MaxRequests int

	RequestTerminateTimeout string
	SlowlogTimeout          string
	SlowlogPath             string

	ErrorLog string

	PHPAdminValues map[string]string
	PHPValues      map[string]string
}

type PoolManager struct {
	TemplatePath string // internal/fpm/templates/pool.tmpl (resolved at build/deploy time)
}

func (m *PoolManager) Render(td PoolData) ([]byte, error) {
	tplPath := m.TemplatePath
	if tplPath == "" {
		tplPath = filepath.Join("internal", "fpm", "templates", "pool.tmpl")
	}
	tpl, err := template.ParseFiles(tplPath)
	if err != nil {
		return nil, fmt.Errorf("parse pool template %s: %w", tplPath, err)
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, td); err != nil {
		return nil, fmt.Errorf("exec pool template: %w", err)
	}
	return buf.Bytes(), nil
}


func writePoolFileAtomic(path string, data []byte) error {
	// util.WriteFileAtomic requires parent dir to exist.
	dir := filepath.Dir(path)
	if err := util.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return util.WriteFileAtomic(path, data, 0644)
}
