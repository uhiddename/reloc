package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uhiddename/reloc/internal/app"
	"github.com/uhiddename/reloc/internal/buildinfo"
	"github.com/uhiddename/reloc/internal/config"
	"github.com/uhiddename/reloc/internal/store"
	"github.com/uhiddename/reloc/migrations"
)

type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

func usagef(format string, a ...any) error {
	return usageError{fmt.Errorf(format, a...)}
}

var errUsage = usageError{errors.New("usage requested")}
var errShown = errors.New("error already shown")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		switch {
		case errors.Is(err, flag.ErrHelp):
			os.Exit(0)
		case errors.Is(err, errShown):
			os.Exit(2)
		}
		if errors.Is(err, errUsage) {
			_, _ = fmt.Fprintf(os.Stderr, "reloc %s\n\n%s\n", buildinfo.Version(), usageText())
		} else {
			_, _ = fmt.Fprintln(os.Stderr, "reloc:", err)
		}
		if _, ok := errors.AsType[usageError](err); ok {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	cmd, rest, ok := findCommand(args)
	if !ok {
		return usagef(
			"unknown command %q, run \"reloc\" for the list",
			strings.Join(args[:min(2, len(args))], " "),
		)
	}
	// version must work anywhere, without a database to connect to
	if cmd.name == "version" {
		return cmd.run(ctx, env{}, rest)
	}
	cfg, err := config.LoadPostgres()
	if err != nil {
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	return cmd.run(ctx, env{app: app.New(store.New(pool)), pool: pool}, rest)
}

func migrate(ctx context.Context, e env, args []string) error {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	dir, err := fs.Sub(migrations.Postgres, "postgres")
	if err != nil {
		return err
	}
	return store.Migrate(ctx, e.pool, dir)
}

func version(ctx context.Context, e env, args []string) error {
	flags := flag.NewFlagSet("version", flag.ContinueOnError)
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	fmt.Println("reloc", buildinfo.Version())
	return nil
}
