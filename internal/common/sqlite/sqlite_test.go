package sqlite_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/sqlite"
)

func openTestDatabase(t *testing.T) (*sqlite.Database, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "debroxy.db")
	database, err := sqlite.Open(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	return database, path
}

func pragma(t *testing.T, conn *sql.Conn, name string) string {
	t.Helper()
	var value string
	require.NoError(t, conn.QueryRowContext(context.Background(), "PRAGMA "+name).Scan(&value))
	return value
}

func TestOpen_writer_pool_has_one_connection(t *testing.T) {
	t.Parallel()
	database, _ := openTestDatabase(t)

	assert.Equal(t, 1, database.Writer.Stats().MaxOpenConnections)
	assert.Equal(t, sqlite.DefaultReaderConnections, database.Reader.Stats().MaxOpenConnections)
}

func TestOpen_every_connection_has_the_configured_pragmas(t *testing.T) {
	t.Parallel()
	database, _ := openTestDatabase(t)
	ctx := context.Background()

	writer, err := database.Writer.Conn(ctx)
	require.NoError(t, err)
	defer func() { assert.NoError(t, writer.Close()) }()
	assert.Equal(t, "1", pragma(t, writer, "foreign_keys"))
	assert.Equal(t, "5000", pragma(t, writer, "busy_timeout"))
	assert.Equal(t, "wal", pragma(t, writer, "journal_mode"))
	assert.Equal(t, "0", pragma(t, writer, "query_only"))

	// Hold every reader connection at once, so each one is checked.
	readers := make([]*sql.Conn, sqlite.DefaultReaderConnections)
	for i := range readers {
		readers[i], err = database.Reader.Conn(ctx)
		require.NoError(t, err)
		defer func() { assert.NoError(t, readers[i].Close()) }()
	}
	for _, reader := range readers {
		assert.Equal(t, "1", pragma(t, reader, "foreign_keys"))
		assert.Equal(t, "5000", pragma(t, reader, "busy_timeout"))
		assert.Equal(t, "1", pragma(t, reader, "query_only"))
	}
}

func TestOpen_replaced_writer_connection_keeps_the_pragmas(t *testing.T) {
	t.Parallel()
	database, _ := openTestDatabase(t)
	ctx := context.Background()

	conn, err := database.Writer.Conn(ctx)
	require.NoError(t, err)
	// Returning ErrBadConn makes database/sql discard the connection, so the
	// following Close reports it as already closed.
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()

	replaced, err := database.Writer.Conn(ctx)
	require.NoError(t, err)
	defer func() { assert.NoError(t, replaced.Close()) }()
	assert.Equal(t, "1", pragma(t, replaced, "foreign_keys"))
	assert.Equal(t, "5000", pragma(t, replaced, "busy_timeout"))
}

func TestReader_rejects_writes(t *testing.T) {
	t.Parallel()
	database, _ := openTestDatabase(t)
	ctx := context.Background()
	_, err := database.Writer.ExecContext(ctx, "CREATE TABLE things (id INTEGER PRIMARY KEY)")
	require.NoError(t, err)

	_, err = database.Reader.ExecContext(ctx, "INSERT INTO things (id) VALUES (1)")
	assert.Error(t, err, "bare exec")

	tx, err := database.Reader.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "INSERT INTO things (id) VALUES (2)")
	assert.Error(t, err, "exec in transaction")
	require.NoError(t, tx.Rollback())
}

func TestWriter_transactions_begin_immediate(t *testing.T) {
	t.Parallel()
	database, path := openTestDatabase(t)
	ctx := context.Background()

	tx, err := database.Writer.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	// A transaction that has run no statement holds the write lock only if
	// it began with BEGIN IMMEDIATE. A second writer must then be refused.
	other, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(50)")
	require.NoError(t, err)
	defer func() { assert.NoError(t, other.Close()) }()
	_, err = other.ExecContext(ctx, "BEGIN IMMEDIATE")
	assert.ErrorContains(t, err, "locked")
}

func TestWriter_serializes_parallel_read_then_write_transactions(t *testing.T) {
	t.Parallel()
	database, _ := openTestDatabase(t)
	ctx := context.Background()
	_, err := database.Writer.ExecContext(ctx, "CREATE TABLE counter (value INTEGER NOT NULL); INSERT INTO counter VALUES (0)")
	require.NoError(t, err)

	const writers = 20
	var wg sync.WaitGroup
	for range writers {
		wg.Go(func() {
			tx, err := database.Writer.BeginTx(ctx, nil)
			if !assert.NoError(t, err) {
				return
			}
			var value int
			assert.NoError(t, tx.QueryRowContext(ctx, "SELECT value FROM counter").Scan(&value))
			_, err = tx.ExecContext(ctx, "UPDATE counter SET value = ?", value+1)
			assert.NoError(t, err)
			assert.NoError(t, tx.Commit())
		})
	}
	wg.Wait()

	var value int
	require.NoError(t, database.Reader.QueryRowContext(ctx, "SELECT value FROM counter").Scan(&value))
	assert.Equal(t, writers, value)
}

func TestReader_reads_while_a_write_transaction_is_open(t *testing.T) {
	t.Parallel()
	database, _ := openTestDatabase(t)
	ctx := context.Background()
	_, err := database.Writer.ExecContext(ctx, "CREATE TABLE things (id INTEGER PRIMARY KEY); INSERT INTO things VALUES (1)")
	require.NoError(t, err)

	tx, err := database.Writer.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, "INSERT INTO things VALUES (2)")
	require.NoError(t, err)

	var count int
	require.NoError(t, database.Reader.QueryRowContext(ctx, "SELECT count(*) FROM things").Scan(&count))
	assert.Equal(t, 1, count, "the reader sees the last committed state")
}

func TestOpen_refuses_a_database_that_is_already_open(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "debroxy.db")
	first, err := sqlite.Open(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)

	_, err = sqlite.Open(context.Background(), sqlite.Config{Path: path})

	var lockedErr sqlite.DatabaseLockedError
	require.ErrorAs(t, err, &lockedErr)
	assert.Equal(t, path+".lock", lockedErr.LockPath)

	require.NoError(t, first.Close())
	second, err := sqlite.Open(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err, "the lock is released on close")
	require.NoError(t, second.Close())
}

func TestOpen_rejects_invalid_config(t *testing.T) {
	t.Parallel()

	_, err := sqlite.Open(context.Background(), sqlite.Config{})
	assert.Error(t, err)

	_, err = sqlite.Open(context.Background(), sqlite.Config{Path: filepath.Join(t.TempDir(), "x.db"), ReaderConnections: -1})
	assert.Error(t, err)
}

func TestMigrate_applies_pending_migrations_once(t *testing.T) {
	t.Parallel()
	database, _ := openTestDatabase(t)
	ctx := context.Background()
	migrations := fstest.MapFS{
		"0001_job_create_things.sql":     {Data: []byte("-- +goose Up\nCREATE TABLE things (id INTEGER PRIMARY KEY);\n\n-- +goose Down\nDROP TABLE things;\n")},
		"0002_identity_create_users.sql": {Data: []byte("-- +goose Up\nCREATE TABLE users (id TEXT PRIMARY KEY);\n\n-- +goose Down\nDROP TABLE users;\n")},
	}

	require.NoError(t, sqlite.Migrate(ctx, database, migrations))
	require.NoError(t, sqlite.Migrate(ctx, database, migrations), "a second run applies nothing")

	var tables int
	require.NoError(t, database.Reader.QueryRowContext(ctx,
		"SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN ('things', 'users')").Scan(&tables))
	assert.Equal(t, 2, tables)
}

func TestMigrate_accepts_an_empty_set(t *testing.T) {
	t.Parallel()
	database, _ := openTestDatabase(t)

	assert.NoError(t, sqlite.Migrate(context.Background(), database, fstest.MapFS{}))
}

func TestInTransaction_commits_on_success_and_rolls_back_on_error(t *testing.T) {
	t.Parallel()
	database, _ := openTestDatabase(t)
	ctx := context.Background()
	_, err := database.Writer.ExecContext(ctx, "CREATE TABLE things (id INTEGER PRIMARY KEY)")
	require.NoError(t, err)

	require.NoError(t, database.InTransaction(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO things VALUES (1)")
		return err
	}))
	failure := errors.New("update function failed")
	err = database.InTransaction(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO things VALUES (2)"); err != nil {
			return err
		}
		return failure
	})
	require.ErrorIs(t, err, failure)

	var ids []int
	rows, err := database.Reader.QueryContext(ctx, "SELECT id FROM things")
	require.NoError(t, err)
	defer func() { assert.NoError(t, rows.Close()) }()
	for rows.Next() {
		var id int
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []int{1}, ids)
}

func TestIsUniqueViolation(t *testing.T) {
	t.Parallel()
	database, _ := openTestDatabase(t)
	ctx := context.Background()
	_, err := database.Writer.ExecContext(ctx, "CREATE TABLE things (id INTEGER PRIMARY KEY, name TEXT UNIQUE); INSERT INTO things VALUES (1, 'a')")
	require.NoError(t, err)

	_, uniqueErr := database.Writer.ExecContext(ctx, "INSERT INTO things VALUES (2, 'a')")
	_, primaryKeyErr := database.Writer.ExecContext(ctx, "INSERT INTO things VALUES (1, 'b')")
	_, otherErr := database.Writer.ExecContext(ctx, "INSERT INTO missing VALUES (1)")

	assert.True(t, sqlite.IsUniqueViolation(fmt.Errorf("wrapped: %w", uniqueErr)))
	assert.True(t, sqlite.IsUniqueViolation(primaryKeyErr))
	assert.False(t, sqlite.IsUniqueViolation(otherErr))
	assert.False(t, sqlite.IsUniqueViolation(nil))
}
