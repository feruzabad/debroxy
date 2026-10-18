package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	commonsqlite "github.com/feruzabad/debroxy/internal/common/sqlite"
	"github.com/feruzabad/debroxy/internal/identity/adapters/out/sqlite/internal/sqlcgen"
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/out"
)

type SQLiteUserRepository struct {
	database *commonsqlite.Database
}

func NewSQLiteUserRepository(database *commonsqlite.Database) SQLiteUserRepository {
	if database == nil {
		panic("nil database")
	}
	return SQLiteUserRepository{database: database}
}

func (r SQLiteUserRepository) CreateUser(
	ctx context.Context,
	createFn func(ctx context.Context, reads out.UserPolicyReads) (*user.User, error),
) error {
	var created *user.User
	err := r.database.InTransaction(ctx, func(ctx context.Context, tx *sql.Tx) error {
		queries := sqlcgen.New(tx)
		u, err := createFn(ctx, sqliteUserPolicyReads{queries: queries})
		if err != nil {
			return err
		}
		if err := queries.InsertUser(ctx, marshalUser(u)); err != nil {
			if commonsqlite.IsUniqueViolation(err) {
				return uniqueViolation(err, u)
			}
			return fmt.Errorf("insert user: %w", err)
		}
		created = u
		return nil
	})
	if err == nil {
		created.ClearEvents()
	}
	return err
}

func (r SQLiteUserRepository) UpdateUser(
	ctx context.Context,
	id user.ID,
	updateFn func(ctx context.Context, u *user.User, reads out.UserPolicyReads) (*user.User, error),
) error {
	var updated *user.User
	err := r.database.InTransaction(ctx, func(ctx context.Context, tx *sql.Tx) error {
		queries := sqlcgen.New(tx)
		row, err := queries.UserByID(ctx, id.String())
		if errors.Is(err, sql.ErrNoRows) {
			return user.NotFoundError{ID: id.String()}
		}
		if err != nil {
			return fmt.Errorf("load user: %w", err)
		}
		loaded, err := unmarshalUser(row)
		if err != nil {
			return err
		}
		u, err := updateFn(ctx, loaded, sqliteUserPolicyReads{queries: queries})
		if err != nil {
			return err
		}
		params := marshalUser(u)
		if err := queries.UpdateUser(ctx, sqlcgen.UpdateUserParams{
			PasswordHash:      params.PasswordHash,
			OidcIssuer:        params.OidcIssuer,
			OidcSubject:       params.OidcSubject,
			Role:              params.Role,
			Status:            params.Status,
			SessionGeneration: params.SessionGeneration,
			ID:                params.ID,
		}); err != nil {
			if commonsqlite.IsUniqueViolation(err) {
				return uniqueViolation(err, u)
			}
			return fmt.Errorf("update user: %w", err)
		}
		updated = u
		return nil
	})
	if err == nil {
		updated.ClearEvents()
	}
	return err
}

func (r SQLiteUserRepository) UserByID(ctx context.Context, id user.ID) (*user.User, error) {
	row, err := sqlcgen.New(r.database.Reader).UserByID(ctx, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, user.NotFoundError{ID: id.String()}
	}
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	return unmarshalUser(row)
}

func (r SQLiteUserRepository) UserByUsername(ctx context.Context, username user.Username) (*user.User, error) {
	row, err := sqlcgen.New(r.database.Reader).UserByUsername(ctx, username.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, user.NotFoundError{ID: username.String()}
	}
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	return unmarshalUser(row)
}

func (r SQLiteUserRepository) UserByOIDCIdentity(ctx context.Context, identity user.OIDCIdentity) (*user.User, error) {
	row, err := sqlcgen.New(r.database.Reader).UserByOIDCIdentity(ctx, oidcParams(identity))
	if errors.Is(err, sql.ErrNoRows) || identity.IsZero() {
		return nil, user.NotFoundError{ID: identity.Subject()}
	}
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	return unmarshalUser(row)
}

func oidcParams(identity user.OIDCIdentity) sqlcgen.UserByOIDCIdentityParams {
	return sqlcgen.UserByOIDCIdentityParams{
		OidcIssuer:  sql.NullString{String: identity.Issuer(), Valid: true},
		OidcSubject: sql.NullString{String: identity.Subject(), Valid: true},
	}
}

// uniqueViolation tells the two unique keys of users apart; the policy
// reads normally catch both before the write.
func uniqueViolation(err error, u *user.User) error {
	if strings.Contains(err.Error(), "oidc") {
		return user.OIDCIdentityTakenError{}
	}
	return user.UsernameTakenError{Username: u.Username().String()}
}

// sqliteUserPolicyReads runs inside the repository's write transaction (R-10).
type sqliteUserPolicyReads struct {
	queries *sqlcgen.Queries
}

func (r sqliteUserPolicyReads) UserCount(ctx context.Context) (int, error) {
	count, err := r.queries.CountUsers(ctx)
	return int(count), err
}

func (r sqliteUserPolicyReads) ActiveAdminCount(ctx context.Context) (int, error) {
	count, err := r.queries.CountActiveAdmins(ctx)
	return int(count), err
}

func (r sqliteUserPolicyReads) OIDCIdentityHolder(ctx context.Context, identity user.OIDCIdentity) (string, bool, error) {
	if identity.IsZero() {
		return "", false, nil
	}
	row, err := r.queries.UserByOIDCIdentity(ctx, oidcParams(identity))
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return row.ID, true, nil
}
