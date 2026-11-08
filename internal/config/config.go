// Package config reads process configuration from environment variables.
// It is the only place that knows their names:
//
//	POSTGRES_DSN            database connection string, without the password
//	POSTGRES_PASSWORD_FILE  file with the database password
//	RELOCD_ADDR             relocd listen address, ip:port
//	RELOCD_TOKEN_FILE       file with the API token
//	RELOCD_SSH_KEY_FILE     ssh key file; unset disables applying configs
package config

import (
	"fmt"
	"net/netip"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	envPostgresDSN          = "POSTGRES_DSN"
	envPostgresPasswordFile = "POSTGRES_PASSWORD_FILE"
	envRelocdAddr           = "RELOCD_ADDR"
	envRelocdTokenFile      = "RELOCD_TOKEN_FILE"
	envRelocdSSHKeyFile     = "RELOCD_SSH_KEY_FILE"
)

// Relocd is the configuration of the relocd daemon.
type Relocd struct {
	Addr       netip.AddrPort
	Token      string
	SSHKeyPath string // empty if not configured
	Postgres   *pgxpool.Config
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
	addr, err := listenAddr(envRelocdAddr)
	if err != nil {
		return Relocd{}, err
	}
	return Relocd{
		Addr:       addr,
		Token:      token,
		SSHKeyPath: os.Getenv(envRelocdSSHKeyFile),
		Postgres:   pg,
	}, nil
}

// LoadPostgres parses the connection string and reads the password file.
func LoadPostgres() (*pgxpool.Config, error) {
	url := os.Getenv(envPostgresDSN)
	if url == "" {
		return nil, fmt.Errorf("config: %s is not set", envPostgresDSN)
	}
	cfg, err := pgxpool.ParseConfig(url)
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
