package config

import (
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// writeSecret writes data to a fresh file and returns its path.
func writeSecret(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// wantErr fails unless err mentions every one of parts.
func wantErr(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("err = nil, want an error mentioning %q", parts)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("err = %v, want it to mention %q", err, p)
		}
	}
}

func TestSecretFile(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{"plain", "s3cret", "s3cret"},
		{"trailing newline", "s3cret\n", "s3cret"},
		{"trailing CRLF", "s3cret\r\n", "s3cret"},
		// only line endings are trimmed: spaces may be part of a secret
		{"spaces kept", " s3 cret ", " s3 cret "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TEST_SECRET_FILE", writeSecret(t, tt.data))
			got, err := secretFile("TEST_SECRET_FILE")
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("secretFile() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSecretFileErrors(t *testing.T) {
	t.Run("not set", func(t *testing.T) {
		t.Setenv("TEST_SECRET_FILE", "")
		_, err := secretFile("TEST_SECRET_FILE")
		wantErr(t, err, "config: TEST_SECRET_FILE is not set")
	})
	t.Run("missing file", func(t *testing.T) {
		t.Setenv("TEST_SECRET_FILE", filepath.Join(t.TempDir(), "missing"))
		_, err := secretFile("TEST_SECRET_FILE")
		wantErr(t, err, "config: TEST_SECRET_FILE:", "no such file")
	})
	for _, data := range []string{"", "\n", "\r\n\n"} {
		t.Run("empty "+strings.ReplaceAll(data, "\n", `\n`), func(t *testing.T) {
			path := writeSecret(t, data)
			t.Setenv("TEST_SECRET_FILE", path)
			_, err := secretFile("TEST_SECRET_FILE")
			wantErr(t, err, "config: TEST_SECRET_FILE:", path, "is empty")
		})
	}
}

// setPostgres sets a valid database configuration with the given password.
func setPostgres(t *testing.T, password string) {
	t.Helper()
	t.Setenv("POSTGRES_DSN", "postgres://reloc@db.example:5432/reloc?sslmode=disable")
	t.Setenv("POSTGRES_PASSWORD_FILE", writeSecret(t, password))
	// pgx falls back to these, keep the tests independent of the machine
	t.Setenv("PGPASSWORD", "")
	t.Setenv("PGPASSFILE", filepath.Join(t.TempDir(), "no-pgpass"))
}

func TestLoadPostgres(t *testing.T) {
	setPostgres(t, "from-file\n")

	cfg, err := LoadPostgres()
	if err != nil {
		t.Fatal(err)
	}
	cc := cfg.ConnConfig
	if cc.Host != "db.example" || cc.Port != 5432 || cc.User != "reloc" || cc.Database != "reloc" {
		t.Errorf("got %s@%s:%d/%s, want the DSN parts", cc.User, cc.Host, cc.Port, cc.Database)
	}
	if cc.Password != "from-file" {
		t.Errorf("password = %q, want the file contents", cc.Password)
	}
	if tz := cc.RuntimeParams["timezone"]; tz != "UTC" {
		t.Errorf("timezone = %q, want UTC", tz)
	}
}

func TestLoadPostgresFileWins(t *testing.T) {
	setPostgres(t, "from-file")
	t.Setenv("POSTGRES_DSN", "postgres://reloc:from-url@db.example/reloc?timezone=Europe/Berlin")
	t.Setenv("PGPASSWORD", "from-env")

	cfg, err := LoadPostgres()
	if err != nil {
		t.Fatal(err)
	}
	if p := cfg.ConnConfig.Password; p != "from-file" {
		t.Errorf("password = %q, want the file to win over the DSN and PGPASSWORD", p)
	}
	if tz := cfg.ConnConfig.RuntimeParams["timezone"]; tz != "UTC" {
		t.Errorf("timezone = %q, want UTC to win over the DSN", tz)
	}
}

func TestLoadPostgresErrors(t *testing.T) {
	t.Run("dsn not set", func(t *testing.T) {
		setPostgres(t, "pw")
		t.Setenv("POSTGRES_DSN", "")
		_, err := LoadPostgres()
		wantErr(t, err, "config: POSTGRES_DSN is not set")
	})
	t.Run("bad dsn", func(t *testing.T) {
		setPostgres(t, "pw")
		t.Setenv("POSTGRES_DSN", "postgres://reloc:url-secret@db.example:notaport/reloc")
		_, err := LoadPostgres()
		wantErr(t, err, "config: POSTGRES_DSN:")
		if err != nil && strings.Contains(err.Error(), "url-secret") {
			t.Errorf("err reveals the password from the DSN: %v", err)
		}
	})
	t.Run("password file not set", func(t *testing.T) {
		setPostgres(t, "pw")
		t.Setenv("POSTGRES_PASSWORD_FILE", "")
		_, err := LoadPostgres()
		wantErr(t, err, "config: POSTGRES_PASSWORD_FILE is not set")
	})
}

// setRelocd sets a valid relocd configuration.
func setRelocd(t *testing.T) {
	t.Helper()
	setPostgres(t, "pw")
	t.Setenv("RELOCD_ADDR", "127.0.0.1:8080")
	t.Setenv("RELOCD_TOKEN_FILE", writeSecret(t, "token\n"))
	t.Setenv("RELOCD_SSH_KEY_FILE", "/keys/relocd")
}

func TestLoadRelocd(t *testing.T) {
	setRelocd(t)

	cfg, err := LoadRelocd()
	if err != nil {
		t.Fatal(err)
	}
	if want := netip.MustParseAddrPort("127.0.0.1:8080"); cfg.Addr != want {
		t.Errorf("Addr = %v, want %v", cfg.Addr, want)
	}
	if cfg.Token != "token" {
		t.Errorf("Token = %q, want the file contents", cfg.Token)
	}
	// the path only: remote reads and checks the key itself
	if cfg.SSHKeyPath != "/keys/relocd" {
		t.Errorf("SSHKeyPath = %q", cfg.SSHKeyPath)
	}
	if cfg.Postgres == nil || cfg.Postgres.ConnConfig.Password != "pw" {
		t.Error("Postgres is not loaded")
	}
}

func TestLoadRelocdNoSSHKey(t *testing.T) {
	setRelocd(t)
	t.Setenv("RELOCD_SSH_KEY_FILE", "")

	cfg, err := LoadRelocd()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SSHKeyPath != "" {
		t.Errorf("SSHKeyPath = %q, want empty", cfg.SSHKeyPath)
	}
}

func TestLoadRelocdAddr(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:8080", "[::]:8080", "[::1]:443"} {
		t.Run(addr, func(t *testing.T) {
			setRelocd(t)
			t.Setenv("RELOCD_ADDR", addr)
			cfg, err := LoadRelocd()
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Addr.String(); got != addr {
				t.Errorf("Addr = %s, want %s", got, addr)
			}
		})
	}
}

func TestLoadRelocdBadAddr(t *testing.T) {
	tests := []struct {
		addr string
		want string
	}{
		{"", "config: RELOCD_ADDR is not set"},
		// the IP is required, so "all interfaces" is always written out
		{":8080", "config: RELOCD_ADDR:"},
		{"localhost:8080", "config: RELOCD_ADDR:"},
		{"127.0.0.1", "config: RELOCD_ADDR:"},
		{"127.0.0.1:0", "config: RELOCD_ADDR: port must not be 0"},
		{"127.0.0.1:65536", "config: RELOCD_ADDR:"},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			setRelocd(t)
			t.Setenv("RELOCD_ADDR", tt.addr)
			_, err := LoadRelocd()
			wantErr(t, err, tt.want)
		})
	}
}

func TestLoadRelocdTokenRequired(t *testing.T) {
	setRelocd(t)
	t.Setenv("RELOCD_TOKEN_FILE", "")
	_, err := LoadRelocd()
	wantErr(t, err, "config: RELOCD_TOKEN_FILE is not set")
}

// The session time zone must reach a real server, not only the config.
func TestLoadPostgresSessionTimeZone(t *testing.T) {
	if os.Getenv("POSTGRES_DSN") == "" {
		t.Skip("POSTGRES_DSN is not set")
	}
	cfg, err := LoadPostgres()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var tz string
	if err := pool.QueryRow(ctx, "show timezone").Scan(&tz); err != nil {
		t.Fatal(err)
	}
	if tz != "UTC" {
		t.Errorf("session timezone = %q, want UTC", tz)
	}
}
