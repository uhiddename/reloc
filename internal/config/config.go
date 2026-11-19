// Package config reads process configuration from environment variables.
// It is the only place that knows their names:
//
//	RELOC_PGDSN                database connection string, without the password
//	RELOC_PGPASSWORD_FILE      file with the database password
//	RELOCD_ADDR                relocd listen address, ip:port
//	RELOCD_TOKEN_FILE          file with the API token
//	RELOCD_SSH_KEY_FILE        ssh key file; unset disables applying configs
//	RELOCD_CF_ACCESS           1 to require a Cloudflare Access JWT on requests, 0 where no Cloudflare is in front; always set
//	RELOCD_CF_ACCESS_ISSUER    team domain, https://<team>.cloudflareaccess.com; only with RELOCD_CF_ACCESS=1
//	RELOCD_CF_ACCESS_AUDIENCE  the Access application's AUD tag; only with RELOCD_CF_ACCESS=1
package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	envPostgresDSN            = "RELOC_PGDSN"
	envPostgresPasswordFile   = "RELOC_PGPASSWORD_FILE"
	envRelocdAddr             = "RELOCD_ADDR"
	envRelocdTokenFile        = "RELOCD_TOKEN_FILE"
	envRelocdSSHKeyFile       = "RELOCD_SSH_KEY_FILE"
	envRelocdCFAccess         = "RELOCD_CF_ACCESS"
	envRelocdCFAccessIssuer   = "RELOCD_CF_ACCESS_ISSUER"
	envRelocdCFAccessAudience = "RELOCD_CF_ACCESS_AUDIENCE"
)

type CFAccess struct {
	Issuer   string
	Audience string
}

// Relocd is the configuration of the relocd daemon.
type Relocd struct {
	Addr       netip.AddrPort
	Token      string
	SSHKeyPath string // empty if not configured
	Postgres   *pgxpool.Config
	CFAccess   *CFAccess // nil if RELOCD_CF_ACCESS=0
}

// LoadRelocd reads the configuration of the relocd daemon.
func LoadRelocd() (Relocd, error) {
	pg, err := LoadPostgres()
	if err != nil {
		return Relocd{}, err
	}
	token, err := secretFile(envRelocdTokenFile)
	if err != nil {
		return Relocd{}, err
	}
	addr, err := LoadRelocdAddr()
	if err != nil {
		return Relocd{}, err
	}
	cfAccess, err := loadCFAccess()
	if err != nil {
		return Relocd{}, err
	}
	return Relocd{
		Addr:       addr,
		Token:      token,
		SSHKeyPath: os.Getenv(envRelocdSSHKeyFile),
		Postgres:   pg,
		CFAccess:   cfAccess,
	}, nil
}

// LoadRelocdAddr reads only relocd's listen address. It is for commands
// that talk to a running relocd, like its probe, and must not require the
// token or the database.
func LoadRelocdAddr() (netip.AddrPort, error) {
	return listenAddr(envRelocdAddr)
}

// LoadPostgres parses the connection string and reads the password file.
func LoadPostgres() (*pgxpool.Config, error) {
	u := os.Getenv(envPostgresDSN)
	if u == "" {
		return nil, fmt.Errorf("config: %s is not set", envPostgresDSN)
	}
	cfg, err := pgxpool.ParseConfig(u)
	if err != nil {
		return nil, fmt.Errorf("config: %s: %w", envPostgresDSN, err)
	}
	password, err := secretFile(envPostgresPasswordFile)
	if err != nil {
		return nil, err
	}
	// the file always wins over a password in the DSN or in PGPASSWORD
	cfg.ConnConfig.Password = password
	// timestamptz values are converted to text and dates in the session
	// time zone; fixing it keeps queries independent of the server settings
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	return cfg, nil
}

// secretFile reads a secret from the file named by the environment variable
// name. The variable must be set and the file must not be empty.
func secretFile(name string) (string, error) {
	path := os.Getenv(name)
	if path == "" {
		return "", fmt.Errorf("config: %s is not set", name)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("config: %s: %w", name, err)
	}
	// editors and echo add a trailing newline
	secret := strings.TrimRight(string(data), "\r\n")
	if secret == "" {
		return "", fmt.Errorf("config: %s: %s is empty", name, path)
	}
	return secret, nil
}

// listenAddr parses the variable name as ip:port. The IP is required, so that
// listening on all interfaces is always written out.
func listenAddr(name string) (netip.AddrPort, error) {
	s := os.Getenv(name)
	if s == "" {
		return netip.AddrPort{}, fmt.Errorf("config: %s is not set", name)
	}
	addr, err := netip.ParseAddrPort(s)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("config: %s: %w", name, err)
	}
	if addr.Port() == 0 {
		return netip.AddrPort{}, fmt.Errorf("config: %s: port must not be 0", name)
	}
	return addr, nil
}

func loadCFAccess() (*CFAccess, error) {
	iss, aud := os.Getenv(envRelocdCFAccessIssuer), os.Getenv(envRelocdCFAccessAudience)
	switch os.Getenv(envRelocdCFAccess) {
	case "0":
		if iss != "" {
			return nil, fmt.Errorf("config: %s is set while %s=0", envRelocdCFAccessIssuer, envRelocdCFAccess)
		}
		if aud != "" {
			return nil, fmt.Errorf("config: %s is set while %s=0", envRelocdCFAccessAudience, envRelocdCFAccess)
		}
		return nil, nil
	case "1":
		if iss == "" {
			return nil, fmt.Errorf("config: %s is not set", envRelocdCFAccessIssuer)
		}
		if aud == "" {
			return nil, fmt.Errorf("config: %s is not set", envRelocdCFAccessAudience)
		}
	default:
		return nil, fmt.Errorf("config: %s must be 0 (off) or 1 (on)", envRelocdCFAccess)
	}

	u, err := url.Parse(iss)
	if err != nil {
		return nil, fmt.Errorf("config: %s: %w", envRelocdCFAccessIssuer, err)
	}
	if u.Host == "" || iss != "https://"+strings.ToLower(u.Hostname()) {
		return nil, fmt.Errorf("config: %s: must be https://<host> with nothing after it", envRelocdCFAccessIssuer)
	}

	return &CFAccess{
		Issuer:   iss,
		Audience: aud,
	}, nil
}
