package web

import (
	"context"
	"html/template"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"mynginx/internal/app"
	"mynginx/internal/config"
	"mynginx/internal/store"
)

const cookieName = "ngm_session"

type Server struct {
	cfg   *config.Config
	paths config.Paths
	st    store.SiteStore
	core  *app.App

	sessions *SessionStore
	tpl      *template.Template
}

func New(cfg *config.Config, paths config.Paths, st store.SiteStore) (*Server, error) {
	core, err := app.New(cfg, paths, st)
	if err != nil {
		return nil, err
	}

	// Minimal templates (keep it dead simple for now)
	tpl := template.New("root")
	template.Must(tpl.New("login").Parse(loginHTML))
	template.Must(tpl.New("sites").Parse(sitesHTML))

	return &Server{
		cfg:      cfg,
		paths:    paths,
		st:       st,
		core:     core,
		sessions: NewSessionStore(12 * time.Hour),
		tpl:      tpl,
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// UI routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/sites", http.StatusFound)
	})
	mux.HandleFunc("/ui/login", s.handleLogin)
	mux.HandleFunc("/ui/logout", s.requireAuth(s.handleLogout))
	mux.HandleFunc("/ui/sites", s.requireAuth(s.handleSites))
	mux.HandleFunc("/ui/apply", s.requireAuth(s.handleApply))

	return mux
}

func (s *Server) Serve(ctx context.Context, listen string) error {
	srv := &http.Server{
		Addr:              listen,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	return srv.ListenAndServe()
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := s.currentSession(r)
		if !ok {
			http.Redirect(w, r, "/ui/login", http.StatusFound)
			return
		}
		next(w, r)
	}
}

func (s *Server) currentSession(r *http.Request) (Session, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil || strings.TrimSpace(c.Value) == "" {
		return Session{}, false
	}
	return s.sessions.Get(strings.TrimSpace(c.Value))
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil, // when behind nginx TLS later, this becomes true
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		_ = s.tpl.ExecuteTemplate(w, "login", map[string]any{"Error": ""})
		return
	case http.MethodPost:
		_ = r.ParseForm()
		username := strings.TrimSpace(r.FormValue("username"))
		pass := r.FormValue("password")

		u, err := s.st.GetPanelUserByUsername(username)
		if err != nil || !u.Enabled {
			_ = s.tpl.ExecuteTemplate(w, "login", map[string]any{"Error": "Invalid credentials"})
			return
		}
		if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(pass)) != nil {
			_ = s.tpl.ExecuteTemplate(w, "login", map[string]any{"Error": "Invalid credentials"})
			return
		}

		sess, err := s.sessions.New(u.ID, u.Username, u.Role)
		if err != nil {
			_ = s.tpl.ExecuteTemplate(w, "login", map[string]any{"Error": "Login failed"})
			return
		}
		_ = s.st.UpdatePanelUserLastLogin(u.ID)
		s.setSessionCookie(w, r, sess.Token)
		http.Redirect(w, r, "/ui/sites", http.StatusFound)
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(cookieName)
	if err == nil && c != nil {
		s.sessions.Delete(c.Value)
	}
	s.clearSessionCookie(w)
	http.Redirect(w, r, "/ui/login", http.StatusFound)
}

func (s *Server) handleSites(w http.ResponseWriter, r *http.Request) {
	items, err := s.core.SiteList(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = s.tpl.ExecuteTemplate(w, "sites", map[string]any{
		"Items": items,
	})
}

func (s *Server) handleApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()
	domain := strings.TrimSpace(r.FormValue("domain"))

	_, err := s.core.Apply(r.Context(), app.ApplyRequest{Domain: domain})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/ui/sites", http.StatusFound)
}

const loginHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>NGM Login</title></head>
<body style="font-family:system-ui; max-width:520px; margin:40px auto;">
  <h2>NGM Panel Login</h2>
  {{if .Error}}<p style="color:#b00;">{{.Error}}</p>{{end}}
  <form method="post" action="/ui/login">
    <div style="margin:10px 0;">
      <label>Username</label><br/>
      <input name="username" style="width:100%; padding:8px;" />
    </div>
    <div style="margin:10px 0;">
      <label>Password</label><br/>
      <input type="password" name="password" style="width:100%; padding:8px;" />
    </div>
    <button style="padding:10px 14px;">Login</button>
  </form>
</body></html>`

const sitesHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>NGM Sites</title></head>
<body style="font-family:system-ui; margin:30px;">
  <div style="display:flex; gap:12px; align-items:center;">
    <h2 style="margin:0;">Sites</h2>
    <form method="post" action="/ui/logout" style="margin-left:auto;">
      <button style="padding:8px 10px;">Logout</button>
    </form>
  </div>

  <p style="opacity:.8;">Apply renders/publishes nginx vhosts and reloads when needed.</p>

  <table cellpadding="8" cellspacing="0" border="1" style="border-collapse:collapse; width:100%;">
    <thead>
      <tr>
        <th align="left">Domain</th>
        <th>Mode</th>
        <th>Enabled</th>
        <th>State</th>
        <th>Last Applied</th>
        <th>PHP</th>
        <th>Actions</th>
      </tr>
    </thead>
    <tbody>
    {{range .Items}}
      <tr>
        <td>{{.Site.Domain}}</td>
        <td align="center">{{.Site.Mode}}</td>
        <td align="center">{{if .Site.Enabled}}yes{{else}}no{{end}}</td>
        <td align="center">{{.State}}</td>
        <td align="center">{{.Last}}</td>
        <td align="center">{{.Site.PHPVersion}}</td>
        <td align="center">
          <form method="post" action="/ui/apply" style="display:inline;">
            <input type="hidden" name="domain" value="{{.Site.Domain}}">
            <button>Apply</button>
          </form>
        </td>
      </tr>
    {{end}}
    </tbody>
  </table>
</body></html>`
