package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"mynginx/internal/config"
	"mynginx/internal/nginx"
	"mynginx/internal/store"
	storesqlite "mynginx/internal/store/sqlite"
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
		if err := cmdSite(st, cfg, args[1:]); err != nil {
			log.Fatalf("site: %v", err)
		}
	default:
		fmt.Printf("Unknown command: %s\n", args[0])
		fmt.Println("Commands:")
		fmt.Println("  site add --user <u> --domain <d> [--mode php|proxy|static] [--php 8.3] [--webroot <path>] [--http3=true|false]")
		fmt.Println("  site list")
		fmt.Println("  site rm --domain <d>")
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

func cmdSite(st store.SiteStore, cfg *config.Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: site <add|list|rm> ...")
	}

	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("site add", flag.ContinueOnError)
		var (
			user   = fs.String("user", "", "Owner username")
			domain = fs.String("domain", "", "Domain (e.g. example.com)")
			mode   = fs.String("mode", "php", "Mode: php|proxy|static")
			phpv   = fs.String("php", cfg.PHPFPM.DefaultVersion, "PHP version (e.g. 8.3)")
			webroot = fs.String("webroot", "", "Webroot path (optional; default derived from user+domain)")
			http3  = fs.Bool("http3", true, "Enable HTTP/3")
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
			// /home/<user>/<sites_root_name>/<domain>/public
			wr = filepath.Join(home, cfg.Hosting.SitesRootName, d, "public")
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
		fmt.Printf("%-25s  %-6s  %-5s  %-40s  %s\n", "DOMAIN", "MODE", "HTTP3", "WEBROOT", "PHP")
		for _, s := range sites {
			fmt.Printf("%-25s  %-6s  %-5v  %-40s  %s\n",
				s.Domain, s.Mode, s.EnableHTTP3, trimLen(s.Webroot, 40), s.PHPVersion)
		}
		return nil

	case "rm":
		fs := flag.NewFlagSet("site rm", flag.ContinueOnError)
		var domain = fs.String("domain", "", "Domain to remove")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *domain == "" {
			return fmt.Errorf("required: --domain")
		}
		d := strings.ToLower(strings.TrimSpace(*domain))
		if err := st.DeleteSiteByDomain(d); err != nil {
			return err
		}
		fmt.Println("OK: site removed from DB:", d)
		return nil

	default:
		return fmt.Errorf("unknown site subcommand: %s", args[0])
	}
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
