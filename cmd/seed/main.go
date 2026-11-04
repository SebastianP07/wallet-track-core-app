// Command seed loads an invented fixture into the local development database.
// It refuses any database that is not on this machine.
package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

const seedTimeout = 30 * time.Second

//go:embed seed.sql
var seedSQL string

func main() {
	if err := run(os.Getenv("DATABASE_URL")); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	fmt.Println("seed: done")
}

func run(databaseURL string) (err error) {
	if databaseURL == "" {
		return errors.New("DATABASE_URL is not set")
	}
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		// pgx errors can echo the connection string, including its password.
		return errors.New("DATABASE_URL is not a valid connection string")
	}
	if err := requireLocalHosts(config); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), seedTimeout)
	defer cancel()

	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() {
		err = errors.Join(err, conn.Close(ctx))
	}()

	err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		_, execErr := tx.Exec(ctx, seedSQL)
		return execErr
	})
	if err != nil {
		return fmt.Errorf("load fixture: %w", err)
	}
	return nil
}

// requireLocalHosts inspects the parsed configuration rather than the raw string,
// so a host given through PGHOST, a query parameter or a fallback list is checked too.
func requireLocalHosts(config *pgx.ConnConfig) error {
	if !isLocalHost(config.Host) {
		return fmt.Errorf("refusing non-local database host %q", config.Host)
	}
	for _, fallback := range config.Fallbacks {
		if !isLocalHost(fallback.Host) {
			return fmt.Errorf("refusing non-local database host %q", fallback.Host)
		}
	}
	return nil
}

func isLocalHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
