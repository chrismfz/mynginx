package sqlite

import (
	"fmt"
	"time"
)

func (s *Store) UpdateApplyResult(domain, status, errMsg, renderHash string) error {
	if domain == "" {
		return fmt.Errorf("domain is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err := s.db.Exec(`
		UPDATE sites
		SET last_applied_at=?,
		    last_apply_status=?,
		    last_apply_error=?,
		    last_render_hash=?,
		    updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE domain=?
	`, now, status, errMsg, renderHash, domain)
	return err
}
