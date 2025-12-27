package fpm

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
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

	ErrorLog string

	PHPAdminValues map[string]string
	PHPValues      map[string]string
}

type PoolManager struct {
	TemplatePath string // internal/fpm/templates/pool.tmpl (resolved at build/deploy time)
}

func (m *PoolManager) Render(td PoolData) ([]byte, error) {
	tpl, err := template.ParseFiles(m.TemplatePath)
	if err != nil {
		return nil, fmt.Errorf("parse pool template: %w", err)
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, td); err != nil {
		return nil, fmt.Errorf("exec pool template: %w", err)
	}
	return buf.Bytes(), nil
}

func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = tmp.Close(); _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(perm); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
