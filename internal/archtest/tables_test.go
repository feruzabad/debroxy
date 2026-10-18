package archtest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/archtest"
)

func TestRepository_respects_table_ownership(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	violations, err := archtest.CheckTableOwnership(root)

	require.NoError(t, err)
	for _, violation := range violations {
		t.Error(violation)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestCheckTableOwnership_finds_violations(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "migrations", "0001_identity_create_users.sql"), "CREATE TABLE users (id TEXT PRIMARY KEY);")
	writeFile(t, filepath.Join(root, "migrations", "0002_job_create_jobs.sql"),
		"CREATE TABLE jobs (id TEXT, owner_id TEXT REFERENCES users (id));\nCREATE INDEX jobs_owner ON jobs (owner_id);")
	writeFile(t, filepath.Join(root, "migrations", "0003_identity_touch_jobs.sql"), "ALTER TABLE jobs ADD COLUMN x TEXT;")
	writeFile(t, filepath.Join(root, "migrations", "0004_billing_create_invoices.sql"), "CREATE TABLE invoices (id TEXT);")
	writeFile(t, filepath.Join(root, "internal", "job", "adapters", "out", "sqlite", "queries", "jobs.sql"),
		"-- name: JobsWithOwners :many\nSELECT * FROM jobs j JOIN users u ON u.id = j.owner_id;")

	violations, err := archtest.CheckTableOwnership(root)

	require.NoError(t, err)
	var messages []string
	for _, violation := range violations {
		messages = append(messages, violation.Error())
	}
	assert.ElementsMatch(t, []string{
		"0002_job_create_jobs.sql: violates ADR 0001 table ownership: context job uses table users of context identity",
		"0003_identity_touch_jobs.sql: violates ADR 0001 table ownership: context identity uses table jobs of context job",
		"0004_billing_create_invoices.sql: violates ADR 0001 table ownership: name must be NNNN_<context>_<change>.sql with a context of [identity provider job delivery]",
		"internal/job/adapters/out/sqlite/queries/jobs.sql: violates ADR 0001 table ownership: context job uses table users of context identity",
	}, messages)
}
