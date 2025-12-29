package store

import "time"


type PanelUser struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         string
	Enabled      bool
	LastLoginAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}


type User struct {
	ID       int64
	Username string
	HomeDir  string
	CreatedAt time.Time
}

type Site struct {
	ID          int64
	UserID      int64
	Domain      string
	Mode        string // "php" | "proxy" | "static"
	Webroot     string
	PHPVersion  string
	EnableHTTP3 bool
	Enabled     bool

	CreatedAt time.Time
	UpdatedAt time.Time

	LastRenderHash  string
	LastAppliedAt   *time.Time
	LastApplyStatus string
	LastApplyError  string
}

type SiteStore interface {
	Migrate() error

	EnsureUser(username, homeDir string) (User, error)
	GetUserByUsername(username string) (User, error)

	UpsertSite(s Site) (Site, error)
	GetSiteByDomain(domain string) (Site, error)
	ListSites() ([]Site, error)
        DisableSiteByDomain(domain string) error

	CreatePanelUser(username, passwordHash, role string, enabled bool) (PanelUser, error)
	GetPanelUserByUsername(username string) (PanelUser, error)
	UpdatePanelUserLastLogin(id int64) error

	Close() error
}

