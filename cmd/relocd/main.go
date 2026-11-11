// Command relocd serves the reloc API.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uhiddename/reloc/internal/api"
	"github.com/uhiddename/reloc/internal/app"
	"github.com/uhiddename/reloc/internal/buildinfo"
	"github.com/uhiddename/reloc/internal/config"
	"github.com/uhiddename/reloc/internal/remote"
	"github.com/uhiddename/reloc/internal/store"
)

const (
	usage = `usage:
  relocd               serve the API
  relocd probe livez   exit 0 if relocd at RELOCD_ADDR is live
  relocd probe readyz  exit 0 if it is ready to serve requests
  relocd version       print version`
	// how long a client may take to send headers; without this limit
	// a slow client holds its connection forever
	readHeaderTimeout = 5 * time.Second
	// how long running requests get to finish on shutdown: longer than an
	// apply, so a rollout doesn't cut one off halfway; keep stop_grace_period
	// in compose.yaml above it
	shutdownTimeout = api.ApplyTimeout + 5*time.Second
)

func main() {
	os.Exit(realMain())
}

func realMain() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch args := os.Args[1:]; {
	case len(args) == 0:
		err = run(ctx)
	case len(args) == 2 && args[0] == "probe" && (args[1] == "livez" || args[1] == "readyz"):
		err = probe(ctx, "/"+args[1])
	case len(args) == 1 && args[0] == "version":
		_, _ = fmt.Println("relocd", buildinfo.Version())
	default:
		_, _ = fmt.Fprintln(os.Stderr, usage)
		// 2: a wrong invocation, unlike 1, which a healthcheck reads as unhealthy
		return 2
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "relocd:", err)
		return 1
	}
	return 0
}

func run(ctx context.Context) error {
	slog.Info("starting", "version", buildinfo.Version())

	cfg, err := config.LoadRelocd()
	if err != nil {
		return err
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg.Postgres)
	if err != nil {
		return err
	}
	defer pool.Close()

	a, err := newApp(store.New(pool), cfg.SSHKeyPath)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Addr.String(),
		Handler:           api.New(a, cfg.Token),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	// buffered: the goroutine must be able to exit if nobody reads the error
	serveErr := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Addr.String())
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		// could not start, for example the port is taken
		return err
	case <-ctx.Done():
	}

	slog.Info("shutting down")
	// a new context: ctx is already canceled, and Shutdown would return at once
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func newApp(st *store.Store, sshKeyPath string) (*app.App, error) {
	if sshKeyPath == "" {
		slog.Warn("no ssh key configured, applying configs is disabled")
		return app.New(st), nil
	}
	auth, err := remote.KeyFileAuth(sshKeyPath)
	if err != nil {
		return nil, err
	}
	return app.New(st).WithAuth(auth), nil
}
