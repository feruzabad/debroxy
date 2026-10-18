package sqlite

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

// Migrate applies all pending goose migrations from migrations on the
// writer pool. Files are named NNNN_<context>_<change>.sql (ADR 0001); the
// leading number is the goose version. An empty set is not an error.
func Migrate(ctx context.Context, database *Database, migrations fs.FS) error {
	provider, err := goose.NewProvider(goose.DialectSQLite3, database.Writer, migrations)
	if errors.Is(err, goose.ErrNoMigrations) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("sqlite: prepare migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("sqlite: apply migrations: %w", err)
	}
	return nil
}
