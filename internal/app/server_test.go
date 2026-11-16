package app

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uhiddename/reloc/internal/store"
)

func TestServerConf(t *testing.T) {
	e := newTestEnv(t)
	srv := e.server(t, "10.66.66.1/24", "")
	ctx := context.Background()

	on := e.createClient(t, srv.ID, "on")
	off := e.createClient(t, srv.ID, "off")
	if err := e.app.SetClientEnabled(ctx, off.ID, false); err != nil {
		t.Fatal(err)
	}

	conf, err := e.app.ServerConf(ctx, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(conf.Peers) != 1 {
		t.Fatalf("peers = %d, want only the enabled client", len(conf.Peers))
	}
	want := netip.PrefixFrom(on.IPv4, 32)
	if !slices.Contains(conf.Peers[0].AllowedIPs, want) {
		t.Errorf("AllowedIPs = %v, want %v", conf.Peers[0].AllowedIPs, want)
	}
}

func TestServerConfNotFound(t *testing.T) {
	e := newTestEnv(t)
	_, err := e.app.ServerConf(context.Background(), -1)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want store.ErrNotFound", err)
	}
}

func TestListServersUnreachable(t *testing.T) {
	// nobody listens on port 1; pgxpool connects lazily, so New succeeds
	db, err := pgxpool.New(context.Background(), "postgres://reloc@127.0.0.1:1/reloc?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = New(store.New(db)).ListServers(context.Background())
	if err == nil || !strings.HasPrefix(err.Error(), "app: list servers: store: list servers: ") {
		t.Fatalf("err = %v, want the app and store prefixes", err)
	}
}
