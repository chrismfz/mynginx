package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mynginx/internal/certs"
	"mynginx/internal/config"
	"mynginx/internal/nginx"
	"mynginx/internal/store"
	storesqlite "mynginx/internal/store/sqlite"
	"mynginx/internal/util/hashx"
	"mynginx/internal/users"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	var cfgPath string
	flag.StringVar(&cfgPath, "c", "config.yaml", "Path to config.yaml")
	flag.Parse()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	paths := cfg.ResolvePaths()

	// Open store early (for CLI commands)
	st, err := storesqlite.Open(cfg.Storage.SQLitePath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	if err := st.Migrate(); err != nil {
		log.Fatalf("store migrate: %v", err)
	}

	args := flag.Args()
	if len(args) == 0 {
		runStatus(cfg, paths)
		return
	}

	switch args[0] {
	case "site":
		if err := cmdSite(st, cfg, paths, args[1:]); err != nil {
			log.Fatalf("site: %v", err)
		}
	case "apply":
		if err := cmdApply(st, cfg, paths, args[1:]); err != nil {
			log.Fatalf("apply: %v", err)
		}
	case "cert":
		if err := cmdCert(cfg, paths, args[1:]); err != nil {
			log.Fatalf("cert: %v", err)
		}
	default:
		fmt.Printf("Unknown command: %s\n", args[0])
		fmt.Println("Commands:")
		fmt.Println("  site add --user <u> --domain <d> [--mode php|proxy|static] [--php 8.3] [--webroot <path>] [--http3=true|false] [--skip-cert]")
		fmt.Println("  site list")
		fmt.Println("  site rm --domain <d>")
		fmt.Println("  apply [--domain <d>] [--all] [--dry-run] [--limit N]")
		fmt.Println("  cert list                          (show all certificates)")
		fmt.Println("  cert info --domain <d>             (show cert details)")
		fmt.Println("  cert issue --domain <d>            (issue/renew certificate)")
		fmt.Println("  cert renew [--domain <d>] [--all] (renew expiring certs)")
		fmt.Println("  cert check [--days 30]             (check expiring soon)")
		os.Exit(2)
	}
}

func runStatus(cfg *config.Config, paths config.Paths) {
	fmt.Println("NGM config loaded OK")
	fmt.Printf("Version: %s  BuildTime: %s\n", Version, BuildTime)

	fmt.Println("---- Nginx ----")
	fmt.Printf("root        : %s\n", paths.NginxRoot)
	fmt.Printf("bin         : %s\n", paths.NginxBin)
	fmt.Printf("main_conf   : %s\n", paths.NginxMainConf)
	fmt.Printf("sites_dir   : %s\n", paths.NginxSitesDir)
	fmt.Printf("staging_dir : %s\n", paths.NginxStageDir)
	fmt.Printf("backup_dir  : %s\n", paths.NginxBackupDir)

	mgr := nginx.NewManager(paths.NginxRoot, paths.NginxBin, paths.NginxMainConf, paths.NginxSitesDir, paths.NginxStageDir, paths.NginxBackupDir)
	if err := mgr.EnsureLayout(); err != nil {
		log.Fatalf("nginx layout: %v", err)
	}
	fmt.Println("---- Layout ----")
	fmt.Println("nginx directories ensured (sites/staging/backup)")

	fmt.Println("---- Nginx Test ----")
	if err := mgr.TestConfig(); err != nil {
		log.Fatalf("nginx test: %v", err)
	}
	fmt.Println("nginx config test OK")

	fmt.Println("---- API ----")
	fmt.Printf("listen      : %s\n", cfg.API.Listen)
	fmt.Printf("allow_ips   : %v\n", cfg.API.AllowIPs)

	fmt.Println("---- Certs ----")
	fmt.Printf("mode        : %s\n", cfg.Certs.Mode)
	fmt.Printf("certbot_bin : %s\n", paths.CertbotBin)
	fmt.Printf("webroot     : %s\n", paths.ACMEWebroot)
	fmt.Printf("live_dir    : %s\n", paths.LetsEncryptLive)

	fmt.Println("---- PHP-FPM ----")
	fmt.Printf("default     : %s\n", cfg.PHPFPM.DefaultVersion)
	fmt.Printf("versions    : %d\n", len(cfg.PHPFPM.Versions))
}

func cmdSite(st store.SiteStore, cfg *config.Config, paths config.Paths, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: site <add|list|rm> ...")
	}

	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("site add", flag.ContinueOnError)
		var (
			user      = fs.String("user", "", "Owner username")
			domain    = fs.String("domain", "", "Domain (e.g. example.com)")
			mode      = fs.String("mode", "php", "Mode: php|proxy|static")
			phpv      = fs.String("php", cfg.PHPFPM.DefaultVersion, "PHP version (e.g. 8.3)")
			webroot   = fs.String("webroot", "", "Webroot path (optional; default derived from user+domain)")
			http3     = fs.Bool("http3", true, "Enable HTTP/3")
			provision = fs.Bool("provision", true, "Create linux user (if missing) + create site dirs")
			skipCert  = fs.Bool("skip-cert", false, "Skip automatic certificate issuance")
		)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *user == "" || *domain == "" {
			return fmt.Errorf("required: --user and --domain")
		}

		d := strings.ToLower(strings.TrimSpace(*domain))
		home := filepath.Join(cfg.Hosting.HomeRoot, *user)

		u, err := st.EnsureUser(*user, home)
		if err != nil {
			return err
		}

		wr := *webroot
		if wr == "" {
			wr = filepath.Join(home, cfg.Hosting.SitesRootName, d, "public")
		}

		// Provision OS user + filesystem layout
		if *provision {
			if err := users.EnsureSystemUser(*user, home); err != nil {
				return err
			}
			webGroup := "www-data"
			if cfg.Hosting.WebGroup != "" {
				webGroup = cfg.Hosting.WebGroup
			}
			if _, err := users.EnsureSiteDirs(*user, home, wr, webGroup); err != nil {
				return err
			}
		}

		// Issue certificate automatically (unless skipped)
		if !*skipCert {
			fmt.Printf("Issuing certificate for %s...\n", d)
			certMgr := certs.NewCertbotManager(
				paths.CertbotBin,
				paths.ACMEWebroot,
				paths.LetsEncryptLive,
				cfg.Certs.Email,
			)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			if err := certMgr.EnsureCertExists(ctx, d); err != nil {
				fmt.Printf("WARNING: Certificate issuance failed: %v\n", err)
				fmt.Println("You can issue it manually later with: cert issue --domain", d)
			} else {
				fmt.Println("Certificate issued successfully!")
			}
		}

		s, err := st.UpsertSite(store.Site{
			UserID:      u.ID,
			Domain:      d,
			Mode:        *mode,
			Webroot:     wr,
			PHPVersion:  *phpv,
			EnableHTTP3: *http3,
			Enabled:     true,
		})

		if err != nil {
			return err
		}

		fmt.Println("OK: site saved")
		fmt.Printf("  domain : %s\n", s.Domain)
		fmt.Printf("  user_id: %d\n", s.UserID)
		fmt.Printf("  mode   : %s\n", s.Mode)
		fmt.Printf("  webroot: %s\n", s.Webroot)
		fmt.Printf("  php    : %s\n", s.PHPVersion)
		fmt.Printf("  http3  : %v\n", s.EnableHTTP3)
		return nil

	case "list":
		sites, err := st.ListSites()
		if err != nil {
			return err
		}
		if len(sites) == 0 {
			fmt.Println("(no sites)")
			return nil
		}

		fmt.Printf("%-25s  %-6s  %-5s  %-9s  %-10s  %-20s  %-40s  %s\n",
			"DOMAIN", "MODE", "HTTP3", "ENABLED", "STATE", "LAST_APPLIED", "WEBROOT", "PHP")

		for _, s := range sites {
			enabledStr := "yes"
			if !s.Enabled {
				enabledStr = "no"
			}
			state, last := siteState(s)
			fmt.Printf("%-25s  %-6s  %-5v  %-9s  %-10s  %-20s  %-40s  %s\n",
				s.Domain, s.Mode, s.EnableHTTP3, enabledStr, state, last, trimLen(s.Webroot, 40), s.PHPVersion)
		}
		return nil

	case "rm":
		fs := flag.NewFlagSet("site rm", flag.ContinueOnError)
		var domain = fs.String("domain", "", "Domain to remove (soft delete)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *domain == "" {
			return fmt.Errorf("required: --domain")
		}
		d := strings.ToLower(strings.TrimSpace(*domain))

		if err := st.DisableSiteByDomain(d); err != nil {
			return err
		}
		fmt.Println("OK: site disabled (pending delete):", d)
		return nil

	default:
		return fmt.Errorf("unknown site subcommand: %s", args[0])
	}
}

func cmdCert(cfg *config.Config, paths config.Paths, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cert <list|info|issue|renew|check> ...")
	}

	certMgr := certs.NewCertbotManager(
		paths.CertbotBin,
		paths.ACMEWebroot,
		paths.LetsEncryptLive,
		cfg.Certs.Email,
	)

	switch args[0] {
	case "list":
		certList, err := certMgr.ListCerts()
		if err != nil {
			return err
		}
		if len(certList) == 0 {
			fmt.Println("(no certificates)")
			return nil
		}

		fmt.Printf("%-30s  %-12s  %-20s  %-20s\n", "DOMAIN", "DAYS LEFT", "NOT BEFORE", "NOT AFTER")
		for _, c := range certList {
			status := fmt.Sprintf("%d days", c.DaysLeft)
			if c.DaysLeft < 0 {
				status = "EXPIRED"
			} else if c.DaysLeft <= 7 {
				status = fmt.Sprintf("%d days (!)", c.DaysLeft)
			}
			fmt.Printf("%-30s  %-12s  %-20s  %-20s\n",
				c.Domain,
				status,
				c.NotBefore.Format("2006-01-02 15:04"),
				c.NotAfter.Format("2006-01-02 15:04"),
			)
		}
		return nil

	case "info":
		fs := flag.NewFlagSet("cert info", flag.ContinueOnError)
		domain := fs.String("domain", "", "Domain")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *domain == "" {
			return fmt.Errorf("required: --domain")
		}

		info, err := certMgr.GetCertInfo(*domain)
		if err != nil {
			return err
		}

		if !info.Exists {
			fmt.Printf("Certificate does not exist for: %s\n", *domain)
			return nil
		}

		fmt.Printf("Domain      : %s\n", info.Domain)
		fmt.Printf("Cert Path   : %s\n", info.CertPath)
		fmt.Printf("Key Path    : %s\n", info.KeyPath)
		fmt.Printf("Not Before  : %s\n", info.NotBefore.Format(time.RFC3339))
		fmt.Printf("Not After   : %s\n", info.NotAfter.Format(time.RFC3339))
		fmt.Printf("Days Left   : %d\n", info.DaysLeft)
		if info.DaysLeft < 0 {
			fmt.Println("Status      : EXPIRED")
		} else if info.DaysLeft <= 7 {
			fmt.Println("Status      : EXPIRING SOON")
		} else if info.DaysLeft <= 30 {
			fmt.Println("Status      : RENEWAL DUE")
		} else {
			fmt.Println("Status      : OK")
		}
		return nil

	case "issue":
		fs := flag.NewFlagSet("cert issue", flag.ContinueOnError)
		domain := fs.String("domain", "", "Domain")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *domain == "" {
			return fmt.Errorf("required: --domain")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		fmt.Printf("Issuing certificate for %s...\n", *domain)
		if err := certMgr.IssueCert(ctx, *domain); err != nil {
			return err
		}
		fmt.Println("Certificate issued successfully!")
		return nil

	case "renew":
		fs := flag.NewFlagSet("cert renew", flag.ContinueOnError)
		domain := fs.String("domain", "", "Domain (optional, renews all if not specified)")
		all := fs.Bool("all", false, "Renew all certificates")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		if *all || *domain == "" {
			fmt.Println("Renewing all certificates...")
			if err := certMgr.RenewAll(ctx); err != nil {
				return err
			}
			fmt.Println("Renewal complete!")
		} else {
			fmt.Printf("Renewing certificate for %s...\n", *domain)
			if err := certMgr.RenewCert(ctx, *domain); err != nil {
				return err
			}
			fmt.Println("Renewal complete!")
		}
		return nil

	case "check":
		fs := flag.NewFlagSet("cert check", flag.ContinueOnError)
		days := fs.Int("days", 30, "Check for certs expiring within N days")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}

		expiring, err := certMgr.CheckExpiringSoon(*days)
		if err != nil {
			return err
		}

		if len(expiring) == 0 {
			fmt.Printf("No certificates expiring within %d days.\n", *days)
			return nil
		}

		fmt.Printf("Certificates expiring within %d days:\n\n", *days)
		fmt.Printf("%-30s  %-12s  %-20s\n", "DOMAIN", "DAYS LEFT", "EXPIRES")
		for _, c := range expiring {
			fmt.Printf("%-30s  %-12d  %-20s\n",
				c.Domain,
				c.DaysLeft,
				c.NotAfter.Format("2006-01-02 15:04"),
			)
		}
		return nil

	default:
		return fmt.Errorf("unknown cert subcommand: %s", args[0])
	}
}

func siteState(s store.Site) (state string, last string) {
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

func siteNeedsApply(s store.Site) bool {
	if !s.Enabled {
		return false
	}
	if s.LastAppliedAt == nil {
		return true
	}
	if s.LastApplyStatus != "ok" {
		return true
	}
	return s.UpdatedAt.After(*s.LastAppliedAt)
}

func trimLen(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func cmdApply(st store.SiteStore, cfg *config.Config, paths config.Paths, args []string) error {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	var (
		domain = fs.String("domain", "", "Apply only this domain (optional)")
		all    = fs.Bool("all", false, "Apply all enabled sites (not only pending)")
		dry    = fs.Bool("dry-run", false, "Show what would be applied, do nothing")
		limit  = fs.Int("limit", 0, "Max number of sites to apply (0 = unlimited)")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	mgr := nginx.NewManager(paths.NginxRoot, paths.NginxBin, paths.NginxMainConf, paths.NginxSitesDir, paths.NginxStageDir, paths.NginxBackupDir)
	if err := mgr.EnsureLayout(); err != nil {
		return fmt.Errorf("nginx layout: %w", err)
	}

	sqlSt, _ := st.(*storesqlite.Store)

	buildTD := func(s store.Site, d string) (nginx.SiteTemplateData, error) {
		siteRoot := filepath.Dir(s.Webroot)
		logsDir := filepath.Join(siteRoot, "logs")

		phpPass := ""
		if s.Mode == "" || s.Mode == "php" {
			ver, ok := cfg.PHPFPM.Versions[s.PHPVersion]
			if !ok {
				return nginx.SiteTemplateData{}, fmt.Errorf("unknown php version %q (not in config.phpfpm.versions)", s.PHPVersion)
			}
			phpSock := filepath.Join(ver.SockDir, "php"+s.PHPVersion+"-fpm.sock")
			phpPass = "unix:" + phpSock
		}

		tlsCert := filepath.Join(cfg.Certs.LetsEncryptLive, d, "fullchain.pem")
		tlsKey := filepath.Join(cfg.Certs.LetsEncryptLive, d, "privkey.pem")

		td := nginx.SiteTemplateData{
			Domain:          d,
			Mode:            s.Mode,
			Webroot:         s.Webroot,
			ACMEWebroot:     cfg.Certs.Webroot,
			EnableHTTP3:     s.EnableHTTP3,
			TLSCert:         tlsCert,
			TLSKey:          tlsKey,
			FrontController: true,
			AccessLog:       filepath.Join(logsDir, "access.log"),
			ErrorLog:        filepath.Join(logsDir, "error.log"),
		}

		if s.Mode == "" || s.Mode == "php" {
			td.PHP = nginx.FastCGICfg{
				Pass: phpPass,
				Cache: nginx.CacheCfg{
					Enabled: true,
					Zone:    "php_cache",
					TTL200:  "1s",
				},
			}
		}

		return td, nil
	}

	if strings.TrimSpace(*domain) != "" {
		return applySingle(mgr, st, cfg, sqlSt, buildTD, *domain, *dry)
	}

	sites, err := st.ListSites()
	if err != nil {
		return err
	}

	var (
		changed []string
		hashes  = map[string]string{}
	)

	appliedCount := 0
	for _, s := range sites {
		if *limit > 0 && appliedCount >= *limit {
			break
		}

		d := strings.ToLower(strings.TrimSpace(s.Domain))

		if !s.Enabled {
			if *dry {
				fmt.Println("dry-run delete:", d)
				appliedCount++
				continue
			}

			ok, err := stageDeleteLiveConf(mgr, d, false)
			if err != nil {
				if sqlSt != nil {
					_ = sqlSt.UpdateApplyResult(d, "fail", "delete live conf failed: "+err.Error(), "")
				}
				fmt.Println("FAIL delete:", d, "-", err)
				continue
			}
			if !ok {
				continue
			}
			fmt.Println("staged delete:", d)
			changed = append(changed, d)
			hashes[d] = ""
			appliedCount++
			continue
		}

		if !*all && !siteNeedsApply(s) {
			continue
		}

		if *dry {
			fmt.Println("dry-run apply:", d)
			appliedCount++
			continue
		}

		td, err := buildTD(s, d)
		if err != nil {
			if sqlSt != nil {
				_ = sqlSt.UpdateApplyResult(d, "fail", err.Error(), "")
			}
			fmt.Println("SKIP:", d, "-", err)
			continue
		}

		outPath, content, err := mgr.RenderSiteToStaging(td)
		renderHash := ""
		if content != nil {
			renderHash = hashx.Sha256Hex(content)
		}
		hashes[d] = renderHash

		if err != nil {
			if sqlSt != nil {
				_ = sqlSt.UpdateApplyResult(d, "fail", err.Error(), renderHash)
			}
			fmt.Println("FAIL render:", d, "-", err)
			continue
		}
		fmt.Println("rendered:", outPath)

		if err := mgr.PublishOnly(d); err != nil {
			if sqlSt != nil {
				_ = sqlSt.UpdateApplyResult(d, "fail", err.Error(), renderHash)
			}
			fmt.Println("FAIL publish:", d, "-", err)
			continue
		}

		changed = append(changed, d)
		appliedCount++
	}

	if *dry {
		fmt.Println("dry-run done.")
		return nil
	}
	if len(changed) == 0 {
		fmt.Println("Nothing to apply (no pending changes).")
		return nil
	}

	if err := mgr.TestConfig(); err != nil {
		rollbackFromBackup(mgr, changed)
		_ = mgr.Reload()
		for _, d := range changed {
			if sqlSt != nil {
				_ = sqlSt.UpdateApplyResult(d, "fail", "nginx -t failed (rolled back): "+err.Error(), hashes[d])
			}
		}
		return fmt.Errorf("nginx -t failed (rolled back): %w", err)
	}

	if err := mgr.Reload(); err != nil {
		rollbackFromBackup(mgr, changed)
		_ = mgr.Reload()
		for _, d := range changed {
			if sqlSt != nil {
				_ = sqlSt.UpdateApplyResult(d, "fail", "nginx reload failed (rolled back): "+err.Error(), hashes[d])
			}
		}
		return fmt.Errorf("nginx reload failed (rolled back): %w", err)
	}

	for _, d := range changed {
		if sqlSt != nil {
			_ = sqlSt.UpdateApplyResult(d, "ok", "", hashes[d])
		}
	}
	fmt.Printf("Applied OK (%d): %s\n", len(changed), strings.Join(changed, ", "))
	return nil
}

func applySingle(
	mgr *nginx.Manager,
	st store.SiteStore,
	cfg *config.Config,
	sqlSt *storesqlite.Store,
	buildTD func(store.Site, string) (nginx.SiteTemplateData, error),
	domain string,
	dry bool,
) error {
	d := strings.ToLower(strings.TrimSpace(domain))
	s, err := st.GetSiteByDomain(d)
	if err != nil {
		return fmt.Errorf("get site: %w", err)
	}

	if dry {
		if !s.Enabled {
			fmt.Println("dry-run delete:", d)
			return nil
		}
		fmt.Println("dry-run apply:", d)
		return nil
	}

	if !s.Enabled {
		ok, err := stageDeleteLiveConf(mgr, d, false)
		if err != nil {
			if sqlSt != nil {
				_ = sqlSt.UpdateApplyResult(d, "fail", "delete live conf failed: "+err.Error(), "")
			}
			return err
		}
		if !ok {
			fmt.Println("Nothing to delete for:", d)
			return nil
		}

		if err := mgr.TestConfig(); err != nil {
			rollbackFromBackup(mgr, []string{d})
			_ = mgr.Reload()
			if sqlSt != nil {
				_ = sqlSt.UpdateApplyResult(d, "fail", "nginx -t failed (rolled back): "+err.Error(), "")
			}
			return fmt.Errorf("nginx -t failed (rolled back): %w", err)
		}

		if err := mgr.Reload(); err != nil {
			rollbackFromBackup(mgr, []string{d})
			_ = mgr.Reload()
			if sqlSt != nil {
				_ = sqlSt.UpdateApplyResult(d, "fail", "nginx reload failed (rolled back): "+err.Error(), "")
			}
			return fmt.Errorf("nginx reload failed (rolled back): %w", err)
		}

		if sqlSt != nil {
			_ = sqlSt.UpdateApplyResult(d, "ok", "", "")
		}
		fmt.Println("deleted OK:", d)
		return nil
	}

	td, err := buildTD(s, d)
	if err != nil {
		return err
	}

	outPath, content, err := mgr.RenderSiteToStaging(td)
	renderHash := ""
	if content != nil {
		renderHash = hashx.Sha256Hex(content)
	}
	if err != nil {
		if sqlSt != nil {
			_ = sqlSt.UpdateApplyResult(d, "fail", err.Error(), renderHash)
		}
		return fmt.Errorf("render: %w", err)
	}
	fmt.Println("rendered:", outPath)

	_, err = mgr.PublishSiteFromStaging(d)
	if err != nil {
		if sqlSt != nil {
			_ = sqlSt.UpdateApplyResult(d, "fail", err.Error(), renderHash)
		}
		return fmt.Errorf("publish: %w", err)
	}

	if sqlSt != nil {
		_ = sqlSt.UpdateApplyResult(d, "ok", "", renderHash)
	}
	fmt.Println("applied OK:", d)
	return nil
}

func stageDeleteLiveConf(mgr *nginx.Manager, domain string, dry bool) (bool, error) {
	live := filepath.Join(mgr.SitesDir, domain+".conf")
	bak := filepath.Join(mgr.BackupDir, domain+".conf.bak")

	if _, err := os.Stat(live); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	if dry {
		return true, nil
	}

	data, err := os.ReadFile(live)
	if err != nil {
		return false, err
	}
	if err := writeFileAtomic(bak, data, 0644); err != nil {
		return false, err
	}
	if err := os.Remove(live); err != nil {
		return false, err
	}
	return true, nil
}

func rollbackFromBackup(mgr *nginx.Manager, domains []string) {
	for _, d := range domains {
		dst := filepath.Join(mgr.SitesDir, d+".conf")
		bak := filepath.Join(mgr.BackupDir, d+".conf.bak")

		if data, err := os.ReadFile(bak); err == nil && len(data) > 0 {
			_ = writeFileAtomic(dst, data, 0644)
			continue
		}
		_ = os.Remove(dst)
	}
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	if err := tmp.Chmod(perm); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmpName, path)
}
