package sqlite

import "mynginx/internal/store"

func (s *Store) ListPendingSites() ([]store.Site, error) {
	rows, err := s.db.Query(`
		SELECT id, user_id, domain, mode, webroot, php_version,
		       enable_http3, enabled,
		       created_at, updated_at,
		       COALESCE(last_render_hash,''), COALESCE(last_apply_status,''), COALESCE(last_apply_error,''),
		       last_applied_at
		FROM sites
		WHERE enabled=1
		  AND (last_applied_at IS NULL
		       OR last_apply_status!='ok'
		       OR updated_at > last_applied_at)
		ORDER BY domain ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// reuse existing scanner by calling s.ListSites() would be heavy; keep simple:
	var out []store.Site
	for rows.Next() {
		var site store.Site
		var created, updated string
		var enableHTTP3, enabled int
		var lastApplied *string // nullable

		if err := rows.Scan(
			&site.ID, &site.UserID, &site.Domain, &site.Mode, &site.Webroot, &site.PHPVersion,
			&enableHTTP3, &enabled,
			&created, &updated,
			&site.LastRenderHash, &site.LastApplyStatus, &site.LastApplyError,
			&lastApplied,
		); err != nil {
			return nil, err
		}

		site.EnableHTTP3 = enableHTTP3 == 1
		site.Enabled = enabled == 1
		// timestamps parsed already in Get/List; not critical for apply
		out = append(out, site)
	}
	return out, rows.Err()
}
