package users

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type SiteDirs struct {
	SiteRoot string
	Public   string
	Logs     string
	Tmp      string
	PHP      string
}

// EnsureSystemUser ensures the Linux user exists. If missing, it will create it (root required).
// NOTE: This is intentionally conservative and only creates the user with a home dir.
func EnsureSystemUser(username, homeDir string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return fmt.Errorf("username is empty")
	}
	if homeDir == "" {
		return fmt.Errorf("homeDir is empty")
	}
	if userExists(username) {
		return nil
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("linux user %q does not exist; run as root to create it", username)
	}

	// Prefer useradd (common). If you want Debian-style adduser, we can switch later.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "useradd", "-m", "-d", homeDir, "-s", "/bin/bash", username)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("useradd failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// EnsureSiteDirs creates the site layout around webroot:
//   <siteRoot>/public (webroot)
//   <siteRoot>/logs
//   <siteRoot>/tmp
//   <siteRoot>/php (future per-user php-fpm pool/config)
//
// It tries to chown to: owner = owner of nearest existing parent, group = webGroup (if exists).
// If not root, it will still mkdir but chown may be skipped.
func EnsureSiteDirs(webroot, webGroup string) (SiteDirs, error) {
	webroot = filepath.Clean(strings.TrimSpace(webroot))
	if webroot == "" || webroot == "/" {
		return SiteDirs{}, fmt.Errorf("invalid webroot %q", webroot)
	}

	siteRoot := filepath.Dir(webroot)
	dirs := SiteDirs{
		SiteRoot: siteRoot,
		Public:   webroot,
		Logs:     filepath.Join(siteRoot, "logs"),
		Tmp:      filepath.Join(siteRoot, "tmp"),
		PHP:      filepath.Join(siteRoot, "php"),
	}

	// Create dirs
	for _, d := range []string{dirs.SiteRoot, dirs.Public, dirs.Logs, dirs.Tmp, dirs.PHP} {
		if err := os.MkdirAll(d, 0750); err != nil {
			return SiteDirs{}, fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	// Create log files (so nginx can open them immediately)
	_ = touchFile(filepath.Join(dirs.Logs, "access.log"), 0640)
	_ = touchFile(filepath.Join(dirs.Logs, "error.log"), 0640)

	// Ownership: infer uid from nearest existing parent
	uid, gid, err := ownerOfNearestExisting(dirs.SiteRoot)
	if err == nil {
		if g, ok := lookupGroupGID(webGroup); ok {
			gid = uint32(g)
		}
		// Best-effort chown (root recommended)
		_ = chownR(dirs.SiteRoot, int(uid), int(gid))
	}

	return dirs, nil
}

func userExists(username string) bool {
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return false
	}
	defer f.Close()
	prefix := username + ":"
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), prefix) {
			return true
		}
	}
	return false
}

func lookupGroupGID(group string) (int, bool) {
	group = strings.TrimSpace(group)
	if group == "" {
		return 0, false
	}
	f, err := os.Open("/etc/group")
	if err != nil {
		return 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) < 3 {
			continue
		}
		if parts[0] == group {
			gid, err := strconv.Atoi(parts[2])
			if err == nil {
				return gid, true
			}
		}
	}
	return 0, false
}

func ownerOfNearestExisting(path string) (uint32, uint32, error) {
	p := path
	for {
		st, err := os.Stat(p)
		if err == nil {
			if sys, ok := st.Sys().(*syscall.Stat_t); ok {
				return sys.Uid, sys.Gid, nil
			}
			return 0, 0, fmt.Errorf("no stat_t for %s", p)
		}
		next := filepath.Dir(p)
		if next == p {
			break
		}
		p = next
	}
	return 0, 0, fmt.Errorf("no existing parent found for %s", path)
}

func chownR(root string, uid, gid int) error {
	return filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// ignore EPERM for non-root runs
		if e := os.Chown(p, uid, gid); e != nil {
			return nil
		}
		return nil
	})
}

func touchFile(path string, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND, perm)
	if err != nil {
		return err
	}
	return f.Close()
}
