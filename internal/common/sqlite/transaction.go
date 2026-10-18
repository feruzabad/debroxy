package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	driver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// InTransaction runs fn in a write transaction on the writer pool, which
// begins with BEGIN IMMEDIATE (R-5). It commits when fn returns nil and
// rolls back otherwise. The commit error is always returned.
func (d *Database) InTransaction(ctx context.Context, fn func(ctx context.Context, tx *sql.Tx) error) (err error) {
	tx, err := d.Writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { err = finishTransaction(tx, err) }()
	return fn(ctx, tx)
}

// finishTransaction commits on success, rolls back on failure, and never
// swallows the commit error (ddd-rules §12.5).
func finishTransaction(tx *sql.Tx, err error) error {
	if err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("rollback: %w", rollbackErr))
		}
		return err
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("commit: %w", commitErr)
	}
	return nil
}

// IsUniqueViolation reports whether err comes from a UNIQUE or PRIMARY KEY
// constraint, so repositories can turn it into a domain error.
func IsUniqueViolation(err error) bool {
	var sqliteErr *driver.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	code := sqliteErr.Code()
	return code == sqlite3.SQLITE_CONSTRAINT_UNIQUE || code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
}
