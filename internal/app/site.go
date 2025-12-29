package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"strconv"

	"mynginx/internal/store"
	"mynginx/internal/users"
)

type SiteAddRequest struct {
	User      string
	Domain    string
	Mode      string // php|proxy|static
	PHP       string
	Webroot   string // optional
	HTTP3     bool
	Provision bool
	SkipCert  bool
	ApplyNow  bool

	// For proxy mode: one per line, e.g. "127.0.0.1:8080" or "10.0.0.2:8080 50"
	ProxyTargets []string

}

type SiteAddResult struct {
	Site     store.Site
	Warnings []string
}

type SiteEditRequest struct {
	Domain string

	// optional fields (empty = keep existing, except booleans via pointers)
	User    string
	Mode    string
	PHP     string
	Webroot string

	HTTP3   *bool
	Enabled *bool

	ApplyNow bool
}

type SiteListItem struct {
	Site  store.Site
	State string // OK|PENDING|ERROR|DISABLED
	Last  string // formatted last applied (or "-")
}

func (a *App) SiteAdd(ctx context.Context, req SiteAddRequest) (SiteAddResult, error) {
	_ = ctx

	var out SiteAddResult

	user := strings.TrimSpace(req.User)
	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	if user == "" || domain == "" {
		return out, fmt.Errorf("required: user and domain")
	}

	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = "php"
	}
	if mode != "php" && mode != "proxy" && mode != "static" {
		return out, fmt.Errorf("invalid mode %q", mode)
	}

	phpv := strings.TrimSpace(req.PHP)
	if phpv == "" {
		phpv = a.cfg.PHPFPM.DefaultVersion
	}

	home := filepath.Join(a.cfg.Hosting.HomeRoot, user)

	u, err := a.st.EnsureUser(user, home)
	if err != nil {
		return out, err
	}

	wr := strings.TrimSpace(req.Webroot)
	if wr == "" {
		wr = filepath.Join(home, a.cfg.Hosting.SitesRootName, domain, "public")
	}

	// Provision OS user + filesystem layout
	if req.Provision {
		if err := users.EnsureSystemUser(user, home); err != nil {
			return out, err
		}
		webGroup := a.cfg.Hosting.WebGroup
		if webGroup == "" {
			webGroup = "www-data"
		}
		if _, err := users.EnsureSiteDirs(user, home, wr, webGroup); err != nil {
			return out, err
		}
	}

	s, err := a.st.UpsertSite(store.Site{
		UserID:      u.ID,
		Domain:      domain,
		Mode:        mode,
		Webroot:     wr,
		PHPVersion:  phpv,
		EnableHTTP3: req.HTTP3,
		Enabled:     true,
	})
	if err != nil {
		return out, err
	}
	out.Site = s

	// If proxy targets were provided on create, persist them before apply.
	if mode == "proxy" && len(req.ProxyTargets) > 0 {
		for _, line := range req.ProxyTargets {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			weight := 100
			addr := line
			// allow "addr weight"
			if parts := strings.Fields(line); len(parts) >= 1 {
				addr = parts[0]
				if len(parts) >= 2 {
					if w, err := strconv.Atoi(parts[1]); err == nil && w > 0 {
						weight = w
					}
				}
			}
			if err := a.st.UpsertProxyTarget(s.ID, addr, weight, false, true); err != nil {
				out.Warnings = append(out.Warnings, "proxy target add failed: "+err.Error())
			}
		}
	}

	// Don't apply proxy site if still no targets.
	if mode == "proxy" && req.ApplyNow {
		ts, err := a.st.ListProxyTargetsBySiteID(s.ID)
		if err != nil || len(ts) == 0 {
			out.Warnings = append(out.Warnings, "proxy site created: add at least 1 proxy target, then click Apply")
			req.ApplyNow = false
		}
	}



	// Bootstrap vhost immediately so HTTP-01 can work (unless disabled).
	if req.ApplyNow {
		if _, err := a.Apply(context.Background(), ApplyRequest{Domain: domain}); err != nil {
			out.Warnings = append(out.Warnings, "apply-now failed: "+err.Error())
		}
	}

	// Issue certificate automatically (unless skipped).
	if !req.SkipCert {
		ctx2, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := a.CertIssue(ctx2, domain, true /* apply */); err != nil {
			out.Warnings = append(out.Warnings, "certificate issuance failed: "+err.Error())
		}
	}

	return out, nil
}

func (a *App) SiteDisable(ctx context.Context, domain string) error {
	_ = ctx
	d := strings.ToLower(strings.TrimSpace(domain))
	if d == "" {
		return fmt.Errorf("domain is required")
	}
	return a.st.DisableSiteByDomain(d)
}

func (a *App) SiteEdit(ctx context.Context, req SiteEditRequest) (store.Site, error) {
	_ = ctx

	d := strings.ToLower(strings.TrimSpace(req.Domain))
	if d == "" {
		return store.Site{}, fmt.Errorf("domain is required")
	}

	cur, err := a.st.GetSiteByDomain(d)
	if err != nil {
		return store.Site{}, err
	}

	// Update user (optional)
	userID := cur.UserID
	if strings.TrimSpace(req.User) != "" {
		user := strings.TrimSpace(req.User)
		home := filepath.Join(a.cfg.Hosting.HomeRoot, user)
		u, err := a.st.EnsureUser(user, home)
		if err != nil {
			return store.Site{}, err
		}
		userID = u.ID
	}

	mode := cur.Mode
	if strings.TrimSpace(req.Mode) != "" {
		mode = strings.TrimSpace(req.Mode)
		if mode != "php" && mode != "proxy" && mode != "static" {
			return store.Site{}, fmt.Errorf("invalid mode %q", mode)
		}
	}

	phpv := cur.PHPVersion
	if strings.TrimSpace(req.PHP) != "" {
		phpv = strings.TrimSpace(req.PHP)
	}

	webroot := cur.Webroot
	if strings.TrimSpace(req.Webroot) != "" {
		webroot = strings.TrimSpace(req.Webroot)
	}

	http3 := cur.EnableHTTP3
	if req.HTTP3 != nil {
		http3 = *req.HTTP3
	}

	enabled := cur.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	updated, err := a.st.UpsertSite(store.Site{
		UserID:      userID,
		Domain:      d,
		Mode:        mode,
		Webroot:     webroot,
		PHPVersion:  phpv,
		EnableHTTP3: http3,
		Enabled:     enabled,
	})
	if err != nil {
		return store.Site{}, err
	}

	if req.ApplyNow {
		_, _ = a.Apply(context.Background(), ApplyRequest{Domain: d})
	}

	return updated, nil
}

func (a *App) SiteList(ctx context.Context) ([]SiteListItem, error) {
	_ = ctx
	sites, err := a.st.ListSites()
	if err != nil {
		return nil, err
	}
	out := make([]SiteListItem, 0, len(sites))
	for _, s := range sites {
		state, last := computeSiteState(s)
		out = append(out, SiteListItem{Site: s, State: state, Last: last})
	}
	return out, nil
}


func (a *App) SiteGet(ctx context.Context, domain string) (store.Site, error) {
	_ = ctx
	d := strings.ToLower(strings.TrimSpace(domain))
	if d == "" {
		return store.Site{}, fmt.Errorf("domain is required")
	}
	return a.st.GetSiteByDomain(d)
}



func computeSiteState(s store.Site) (state string, last string) {
	last = "-"
	if s.LastAppliedAt != nil {
		last = s.LastAppliedAt.Format("2006-01-02 15:04")
	}

	if !s.Enabled {
		return "DISABLED", last
	}
	if s.LastApplyStatus == "fail" {
		return "ERROR", last
	}
	if siteNeedsApply(s) {
		return "PENDING", last
	}
	if s.LastApplyStatus == "ok" {
		return "OK", last
	}
	return "PENDING", last
}
