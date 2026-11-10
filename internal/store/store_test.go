package store

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uhiddename/reloc/internal/config"
	"github.com/uhiddename/reloc/migrations"
)

// testStore connects to the database from the environment, see package
// config, and applies migrations. Tests that use it are skipped when
// RELOC_PGDSN is not set; any other configuration error fails them.
func testStore(t *testing.T) *Store {
	t.Helper()
	if os.Getenv("RELOC_PGDSN") == "" {
		t.Skip("RELOC_PGDSN is not set")
	}
	ctx := context.Background()

	cfg, err := config.LoadPostgres()
	if err != nil {
		t.Fatal(err)
	}
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)

	dir, err := fs.Sub(migrations.Postgres, "postgres")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db, dir); err != nil {
		t.Fatal(err)
	}
	return New(db)
}

// unreachableStore returns a Store whose pool points at a port nobody
// listens on. pgxpool connects lazily, so creating it succeeds.
func unreachableStore(t *testing.T) *Store {
	t.Helper()
	db, err := pgxpool.New(context.Background(), "postgres://reloc@127.0.0.1:1/reloc?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return New(db)
}

func TestPing(t *testing.T) {
	s := testStore(t)
	if err := s.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPingUnreachable(t *testing.T) {
	err := unreachableStore(t).Ping(context.Background())
	if err == nil || !strings.HasPrefix(err.Error(), "store: ping: ") {
		t.Fatalf("err = %v, want a store: ping: error", err)
	}
}

// The caller's context bounds Ping: /readyz relies on it for its timeout.
func TestPingCanceled(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Ping(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestPingClosedPool(t *testing.T) {
	s := testStore(t)
	s.db.Close()
	if err := s.Ping(context.Background()); err == nil {
		t.Fatal("Ping on a closed pool succeeded")
	}
}
