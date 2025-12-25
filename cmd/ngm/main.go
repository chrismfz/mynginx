package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"mynginx/internal/config"
	"mynginx/internal/nginx"
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

	fmt.Println("NGM config loaded OK")
	fmt.Println("---- Nginx ----")
	fmt.Printf("root        : %s\n", paths.NginxRoot)
	fmt.Printf("bin         : %s\n", paths.NginxBin)
	fmt.Printf("main_conf   : %s\n", paths.NginxMainConf)
	fmt.Printf("sites_dir   : %s\n", paths.NginxSitesDir)
	fmt.Printf("staging_dir : %s\n", paths.NginxStageDir)
	fmt.Printf("backup_dir  : %s\n", paths.NginxBackupDir)

        // Ensure nginx generated directories exist (sites/staging/backup)
        mgr := nginx.NewManager(
                paths.NginxRoot,
                paths.NginxBin,
                paths.NginxMainConf,
                paths.NginxSitesDir,
                paths.NginxStageDir,
                paths.NginxBackupDir,
        )
        if err := mgr.EnsureLayout(); err != nil {
                log.Fatalf("nginx layout: %v", err)
        }
        fmt.Println("---- Layout ----")
        fmt.Println("nginx directories ensured (sites/staging/backup)")

        fmt.Println("---- Nginx Test ----")
        if err := mgr.TestConfig(); err != nil {
                log.Fatalf("nginx -t failed: %v", err)
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



site := nginx.SiteTemplateData{
    Domain:      "demo.local",
    Mode:        "php",
    Webroot:     "/opt/nginx/html",
    ACMEWebroot: cfg.Certs.Webroot,
    EnableHTTP3: true,
    TLSCert:     "/etc/letsencrypt/live/quic.myip.gr/fullchain.pem",
    TLSKey:      "/etc/letsencrypt/live/quic.myip.gr/privkey.pem",
    FrontController: true,
    PHP: nginx.FastCGICfg{
        Pass: "unix:/run/php/php8.3-fpm.sock",
        Cache: nginx.CacheCfg{Enabled: true, Zone: "php_cache", TTL200: "1s"},
    },
}
out, err := mgr.RenderSiteToStaging(site)
if err != nil { log.Fatalf("render: %v", err) }
fmt.Println("rendered:", out)


	// keep exit code 0
	os.Exit(0)
}
