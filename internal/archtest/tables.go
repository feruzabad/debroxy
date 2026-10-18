package archtest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Table ownership (ADR 0001, DEC-13): every table belongs to the context
// whose migration created it. A migration touches only its own context's
// tables and names that context in its file name; a context's sqlc queries
// name only its own tables; no foreign key crosses contexts.

var (
	migrationName = regexp.MustCompile(`^(\d{4})_([a-z]+)_[a-z0-9_]+\.sql$`)
	createdTable  = regexp.MustCompile(`(?i)\bCREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z_][a-z0-9_]*)`)
	// tableReference finds table names after the SQL keywords that introduce one.
	tableReference = regexp.MustCompile(`(?i)\b(?:CREATE\s+TABLE(?:\s+IF\s+NOT\s+EXISTS)?|ALTER\s+TABLE|DROP\s+TABLE(?:\s+IF\s+EXISTS)?|INSERT\s+INTO|UPDATE|DELETE\s+FROM|FROM|JOIN|REFERENCES|INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?[a-z_][a-z0-9_]*\s+ON)\s+([a-z_][a-z0-9_]*)`)
	sqlComment     = regexp.MustCompile(`--[^\n]*`)
)

// TableOwnershipError reports a breach of the table ownership rule.
type TableOwnershipError struct {
	File   string
	Reason string
}

func (e TableOwnershipError) Error() string {
	return fmt.Sprintf("%s: violates ADR 0001 table ownership: %s", e.File, e.Reason)
}

// CheckTableOwnership checks migrations/*.sql and each context's sqlc
// queries under internal/<context>/adapters/out/sqlite/queries.
func CheckTableOwnership(root string) ([]error, error) {
	migrationFiles, err := filepath.Glob(filepath.Join(root, "migrations", "*.sql"))
	if err != nil {
		return nil, err
	}
	sort.Strings(migrationFiles)
	owners := map[string]string{}
	var violations []error
	for _, path := range migrationFiles {
		name := filepath.Base(path)
		match := migrationName.FindStringSubmatch(name)
		if match == nil || !slices.Contains(BoundedContexts, match[2]) {
			violations = append(violations, TableOwnershipError{File: name, Reason: fmt.Sprintf("name must be NNNN_<context>_<change>.sql with a context of %v", BoundedContexts)})
			continue
		}
		content, err := readSQL(path)
		if err != nil {
			return nil, err
		}
		context := match[2]
		for _, created := range createdTable.FindAllStringSubmatch(content, -1) {
			table := strings.ToLower(created[1])
			if owner, exists := owners[table]; exists && owner != context {
				violations = append(violations, TableOwnershipError{File: name, Reason: fmt.Sprintf("table %s already belongs to %s", table, owner)})
			}
			owners[table] = context
		}
		violations = append(violations, checkReferences(name, content, context, owners)...)
	}
	for _, context := range BoundedContexts {
		queryFiles, err := filepath.Glob(filepath.Join(root, "internal", context, "adapters", "out", "sqlite", "queries", "*.sql"))
		if err != nil {
			return nil, err
		}
		for _, path := range queryFiles {
			content, err := readSQL(path)
			if err != nil {
				return nil, err
			}
			rel, _ := filepath.Rel(root, path)
			violations = append(violations, checkReferences(rel, content, context, owners)...)
		}
	}
	return violations, nil
}

func checkReferences(file, content, context string, owners map[string]string) []error {
	var violations []error
	seen := map[string]bool{}
	for _, reference := range tableReference.FindAllStringSubmatch(content, -1) {
		table := strings.ToLower(reference[1])
		// "DO UPDATE SET" (upsert) and "excluded" are SQL syntax, not tables.
		if seen[table] || strings.HasPrefix(table, "sqlite_") || table == "excluded" || table == "set" {
			continue
		}
		seen[table] = true
		owner, known := owners[table]
		switch {
		case !known:
			violations = append(violations, TableOwnershipError{File: file, Reason: fmt.Sprintf("table %s is created by no earlier migration", table)})
		case owner != context:
			violations = append(violations, TableOwnershipError{File: file, Reason: fmt.Sprintf("context %s uses table %s of context %s", context, table, owner)})
		}
	}
	return violations
}

func readSQL(path string) (string, error) {
	content, err := os.ReadFile(path) //nolint:gosec // paths come from globbing the repository under test
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return sqlComment.ReplaceAllString(string(content), ""), nil
}
