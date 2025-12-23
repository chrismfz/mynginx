# NGM (Nginx Go Manager)

Go control-plane for a custom Nginx install (e.g. `/opt/nginx`) that manages:
- Domains / vhosts (one generated `.conf` per domain)
- Certificates (Certbot HTTP-01 via webroot, MVP)
- Isolated PHP-FPM pools with **multiple PHP versions** (e.g. `php8.3-fpm`, `php8.4-fpm`, …)
- Safe apply pipeline: stage → `nginx -t` → atomic swap → reload → rollback

API first, GUI later.

---

## Project state

```text
NGM-STATE
Date: 2025-12-23
Goal: Structure

Environment:
- OS: Debian
- Nginx root: /opt/nginx
- Nginx bin: /opt/nginx/sbin/nginx
- Nginx main conf: /opt/nginx/conf/nginx.conf
- Sites dir (generated): /opt/nginx/conf/sites
- Webroot (ACME): /opt/nginx/html
- Certs: /etc/letsencrypt/live
- PHP: multi-version (Sury) php8.3-fpm, php8.4-fpm, …

Decisions (locked):
- Config: YAML (+ env overrides later)
- State store: SQLite
- Vhost model: 1 file per domain in sites/*.conf
- FPM isolation: per-domain pool (socket per domain)
- Cert issuance: certbot exec (MVP), ACME-native later
- Apply pipeline: stage → nginx -t → atomic swap → reload → rollback

Current status:
- Repo created? (yes/no)
- Skeleton packages created? (list)
- Endpoints implemented: (list)
- Templates ready: (site.tmpl/pool.tmpl) (yes/no)

Next tasks (top 5):
1)
2)
3)
4)
5)

Open questions/risks:
- (root vs helper, permissions, SELinux/AppArmor, etc.)
```

---

## Conventions

### Paths / layout
- Docroot: `/home/<user>/sites/<domain>/public`
- Vhost conf: `/opt/nginx/conf/sites/<domain>.conf`

### Pool / socket naming
- `poolName`: `u_<user>__d_<domain_sanitized>`
  - Example: `u_chris__d_quic_myip_gr`
- Socket: `/run/php/php<ver>-fpm-<poolName>.sock`
  - Example: `/run/php/php8.4-fpm-u_chris__d_quic_myip_gr.sock`

Domain sanitization: lowercase; `.` → `_`; keep `-`.

---

## MVP Definition of Done (DoD)

- [ ] `POST /users` creates a system user + base directories
- [ ] `POST /domains` creates docroot + vhost config + optional PHP pool
- [ ] Domain supports `php_version` (8.3 / 8.4 / …) or `none`
- [ ] Certbot issuance via webroot + uses `/etc/letsencrypt/live/<domain>`
- [ ] Nginx apply is atomic and safe (`nginx -t` before reload)
- [ ] Rollback on failure (keep last-known-good configs)
- [ ] Audit log for changes (who did what/when)
- [ ] `GET /health` reports: nginx ok, last reload, cert expiry, errors

---

## Configuration

See: `config.example.yaml` (copy to `config.yaml` and edit)

### Run (planned)
```bash
./ngm daemon -c ./config.yaml
```

---

## Changelog (append-only)

### 2025-12-23
- + Agreed architecture and conventions
- + Multi-PHP model: per-domain pools, Sury services
- + Certbot MVP (HTTP-01 webroot)
- - TODO: implement config loader + nginx apply pipeline + API skeleton
