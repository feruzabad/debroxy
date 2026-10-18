package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	commonsqlite "github.com/feruzabad/debroxy/internal/common/sqlite"
	"github.com/feruzabad/debroxy/internal/identity/adapters/out/sqlite/internal/sqlcgen"
	"github.com/feruzabad/debroxy/internal/identity/domain/session"
)

type SQLiteSessionRepository struct {
	database *commonsqlite.Database
}

func NewSQLiteSessionRepository(database *commonsqlite.Database) SQLiteSessionRepository {
	if database == nil {
		panic("nil database")
	}
	return SQLiteSessionRepository{database: database}
}

func (r SQLiteSessionRepository) AddSession(ctx context.Context, s *session.Session) error {
	err := r.database.InTransaction(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := sqlcgen.New(tx).InsertSession(ctx, marshalSession(s)); err != nil {
			return fmt.Errorf("insert session: %w", err)
		}
		return nil
	})
	if err == nil {
		s.ClearEvents()
	}
	return err
}

func (r SQLiteSessionRepository) UpdateSession(
	ctx context.Context,
	id session.ID,
	updateFn func(ctx context.Context, s *session.Session) (*session.Session, error),
) error {
	var updated *session.Session
	err := r.database.InTransaction(ctx, func(ctx context.Context, tx *sql.Tx) error {
		queries := sqlcgen.New(tx)
		row, err := queries.SessionByID(ctx, id.String())
		if errors.Is(err, sql.ErrNoRows) {
			return session.NotFoundError{ID: id.String()}
		}
		if err != nil {
			return fmt.Errorf("load session: %w", err)
		}
		loaded, err := unmarshalSession(row)
		if err != nil {
			return err
		}
		s, err := updateFn(ctx, loaded)
		if err != nil {
			return err
		}
		params := marshalSession(s)
		if err := queries.UpdateSession(ctx, sqlcgen.UpdateSessionParams{
			UserSessionGeneration: params.UserSessionGeneration,
			LastSeenAt:            params.LastSeenAt,
			IdleExpiresAt:         params.IdleExpiresAt,
			RevokedAt:             params.RevokedAt,
			ID:                    params.ID,
		}); err != nil {
			return fmt.Errorf("update session: %w", err)
		}
		updated = s
		return nil
	})
	if err == nil {
		updated.ClearEvents()
	}
	return err
}

func (r SQLiteSessionRepository) PruneSessions(ctx context.Context, now time.Time) (int, error) {
	var pruned int64
	err := r.database.InTransaction(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		pruned, err = sqlcgen.New(tx).PruneSessions(ctx, sqlcgen.PruneSessionsParams{AbsoluteExpiresAt: toMillis(now), IdleExpiresAt: toMillis(now)})
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("prune sessions: %w", err)
	}
	return int(pruned), nil
}
