// Package sqlite opens the one SQLite database that all contexts share
// (ADR 0002, DEC-42): a writer pool with a single connection whose
// transactions begin with BEGIN IMMEDIATE, a read-only reader pool, and an
// exclusive start lock. cmd opens it once and passes it to every context's
// service; it owns no tables and no queries.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
)

// busyTimeoutMilliseconds is fixed by DEC-42.
const busyTimeoutMilliseconds = 5000

// DefaultReaderConnections sizes the reader pool when Config leaves it zero.
const DefaultReaderConnections = 4

// Config locates the database file.
type Config struct {
	Path string
	// ReaderConnections is the reader pool size; a tuning value (ADR 0002 rule 2).
	ReaderConnections int
}

// Database is the shared pair of pools. Writer serves every write
// transaction; Reader serves queries and read models and rejects writes.
type Database struct {
	Writer *sql.DB
	Reader *sql.DB
	lock   *startLock
}

// Open takes the start lock, then opens both pools. The writer is opened
// and pinged first so that WAL mode is set before any reader connects.
func Open(ctx context.Context, cfg Config) (*Database, error) {
	if cfg.Path == "" {
		return nil, errors.New("sqlite: empty database path")
	}
	if cfg.ReaderConnections == 0 {
		cfg.ReaderConnections = DefaultReaderConnections
	}
	if cfg.ReaderConnections < 0 {
		return nil, fmt.Errorf("sqlite: reader connections must be positive, got %d", cfg.ReaderConnections)
	}

	lock, err := acquireStartLock(cfg.Path + ".lock")
	if err != nil {
		return nil, err
	}
	database := &Database{lock: lock}

	database.Writer, err = openPool(ctx, writerDSN(cfg.Path), 1)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("sqlite: open writer pool: %w", err), database.Close())
	}
	database.Reader, err = openPool(ctx, readerDSN(cfg.Path), cfg.ReaderConnections)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("sqlite: open reader pool: %w", err), database.Close())
	}
	return database, nil
}

// Close closes both pools and releases the start lock.
func (d *Database) Close() error {
	var errs []error
	for _, pool := range []*sql.DB{d.Reader, d.Writer} {
		if pool != nil {
			errs = append(errs, pool.Close())
		}
	}
	if d.lock != nil {
		errs = append(errs, d.lock.release())
	}
	return errors.Join(errs...)
}

// openPool keeps every connection open for the life of the process, so the
// per-connection settings of the DSN are applied once per connection.
func openPool(ctx context.Context, dsn string, connections int) (*sql.DB, error) {
	pool, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(connections)
	pool.SetMaxIdleConns(connections)
	pool.SetConnMaxLifetime(0)
	pool.SetConnMaxIdleTime(0)
	if err := pool.PingContext(ctx); err != nil {
		return nil, errors.Join(err, pool.Close())
	}
	return pool, nil
}

// writerDSN: BEGIN IMMEDIATE for every transaction (R-5), WAL, and the
// per-connection pragmas of ADR 0002 rule 3.
func writerDSN(path string) string {
	return dsn(path, "_txlock=immediate", "_pragma=journal_mode(WAL)")
}

// readerDSN: query_only rejects every write on the reader pool.
func readerDSN(path string) string {
	return dsn(path, "_pragma=query_only(1)")
}

func dsn(path string, extra ...string) string {
	params := []string{
		"_pragma=foreign_keys(1)",
		fmt.Sprintf("_pragma=busy_timeout(%d)", busyTimeoutMilliseconds),
	}
	params = append(params, extra...)
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + strings.Join(params, "&")
}

// DatabaseLockedError means another process holds the start lock.
type DatabaseLockedError struct {
	LockPath string
}

func (e DatabaseLockedError) Error() string {
	return fmt.Sprintf("sqlite: database is in use by another debroxy process (lock %s is held)", e.LockPath)
}

type startLock struct {
	file *os.File
}

func (l *startLock) release() error {
	// Closing the descriptor releases the flock.
	return l.file.Close()
}
