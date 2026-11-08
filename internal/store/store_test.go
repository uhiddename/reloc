package store

import (
	"context"
	"io/fs"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uhiddename/reloc/internal/config"
	"github.com/uhiddename/reloc/migrations"
)

// testStore connects to the database from the environment, see package
// config, and applies migrations. Tests that use it are skipped when
// POSTGRES_DSN is not set; any other configuration error fails them.
func testStore(t *testing.T) *Store {
	t.Helper()
	if os.Getenv("POSTGRES_DSN") == "" {
		t.Skip("POSTGRES_DSN is not set")
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
