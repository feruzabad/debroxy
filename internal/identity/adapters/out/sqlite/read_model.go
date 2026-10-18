package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	commonsqlite "github.com/feruzabad/debroxy/internal/common/sqlite"
	"github.com/feruzabad/debroxy/internal/identity/adapters/out/sqlite/internal/sqlcgen"
	"github.com/feruzabad/debroxy/internal/identity/domain/session"
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
)

// SQLiteIdentityReadModel answers the identity queries from the reader pool.
// It implements AuthenticatedActorReadModel, UsersReadModel,
// UserDetailsReadModel and UserStatusReadModel.
type SQLiteIdentityReadModel struct {
	queries *sqlcgen.Queries
}

func NewSQLiteIdentityReadModel(database *commonsqlite.Database) SQLiteIdentityReadModel {
	if database == nil {
		panic("nil database")
	}
	return SQLiteIdentityReadModel{queries: sqlcgen.New(database.Reader)}
}

func (r SQLiteIdentityReadModel) SessionForAuthentication(ctx context.Context, tokenHash string) (in.SessionForAuthentication, error) {
	row, err := r.queries.SessionForAuthentication(ctx, tokenHash)
	if errors.Is(err, sql.ErrNoRows) {
		return in.SessionForAuthentication{}, session.ErrSessionExpired
	}
	if err != nil {
		return in.SessionForAuthentication{}, fmt.Errorf("load session: %w", err)
	}
	return in.SessionForAuthentication{
		SessionID:             row.SessionID,
		UserID:                row.UserID,
		Role:                  row.Role,
		UserStatus:            row.Status,
		UserSessionGeneration: int(row.SessionGeneration),
		SessionGeneration:     int(row.UserSessionGeneration),
		LastSeenAt:            fromMillis(row.LastSeenAt),
		AbsoluteExpiresAt:     fromMillis(row.AbsoluteExpiresAt),
		IdleExpiresAt:         fromMillis(row.IdleExpiresAt),
		RevokedAt:             fromNullMillis(row.RevokedAt),
	}, nil
}

func (r SQLiteIdentityReadModel) Users(ctx context.Context) ([]in.UserSummary, error) {
	rows, err := r.queries.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	summaries := make([]in.UserSummary, 0, len(rows))
	for _, row := range rows {
		summaries = append(summaries, userRowToSummary(row))
	}
	return summaries, nil
}

func (r SQLiteIdentityReadModel) UserDetails(ctx context.Context, userID string) (in.UserSummary, error) {
	row, err := r.queries.UserByID(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return in.UserSummary{}, user.NotFoundError{ID: userID}
	}
	if err != nil {
		return in.UserSummary{}, fmt.Errorf("load user: %w", err)
	}
	return userRowToSummary(row), nil
}

func (r SQLiteIdentityReadModel) UserStatus(ctx context.Context, userID string) (in.UserStatusResult, error) {
	row, err := r.queries.UserByID(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return in.UserStatusResult{}, nil
	}
	if err != nil {
		return in.UserStatusResult{}, fmt.Errorf("load user: %w", err)
	}
	return in.UserStatusResult{Exists: true, Active: row.Status == user.StatusActive.String()}, nil
}
