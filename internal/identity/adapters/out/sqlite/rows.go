// Package sqlite stores the identity context in the shared SQLite database
// (ADR 0002). Rows are the sqlc structs; marshal/unmarshal functions
// translate them to and from the domain (ddd-rules OUT-1).
package sqlite

import (
	"database/sql"
	"time"

	"github.com/feruzabad/debroxy/internal/identity/adapters/out/sqlite/internal/sqlcgen"
	"github.com/feruzabad/debroxy/internal/identity/domain/session"
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
)

// Timestamps are stored as Unix milliseconds, UTC.
func toMillis(t time.Time) int64 { return t.UnixMilli() }

func fromMillis(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

func toNullMillis(t time.Time) sql.NullInt64 {
	if t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.UnixMilli(), Valid: true}
}

func fromNullMillis(ms sql.NullInt64) time.Time {
	if !ms.Valid {
		return time.Time{}
	}
	return fromMillis(ms.Int64)
}

func toNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func marshalUser(u *user.User) sqlcgen.InsertUserParams {
	return sqlcgen.InsertUserParams{
		ID:                u.ID().String(),
		Username:          u.Username().String(),
		PasswordHash:      toNullString(u.PasswordHash().String()),
		OidcIssuer:        toNullString(u.OIDCIdentity().Issuer()),
		OidcSubject:       toNullString(u.OIDCIdentity().Subject()),
		Role:              u.Role().String(),
		Status:            u.Status().String(),
		SessionGeneration: int64(u.SessionGeneration()),
		CreatedAt:         toMillis(u.CreatedAt()),
	}
}

func unmarshalUser(row sqlcgen.User) (*user.User, error) {
	id, err := user.NewID(row.ID)
	if err != nil {
		return nil, err
	}
	username, err := user.NewUsername(row.Username)
	if err != nil {
		return nil, err
	}
	role, err := user.NewRoleFromString(row.Role)
	if err != nil {
		return nil, err
	}
	status, err := user.NewStatusFromString(row.Status)
	if err != nil {
		return nil, err
	}
	var hash user.PasswordHash
	if row.PasswordHash.Valid {
		if hash, err = user.NewPasswordHash(row.PasswordHash.String); err != nil {
			return nil, err
		}
	}
	var oidcIdentity user.OIDCIdentity
	if row.OidcIssuer.Valid {
		if oidcIdentity, err = user.NewOIDCIdentity(row.OidcIssuer.String, row.OidcSubject.String); err != nil {
			return nil, err
		}
	}
	return user.UnmarshalUserFromDatabase(id, username, role, status, hash, oidcIdentity,
		int(row.SessionGeneration), fromMillis(row.CreatedAt))
}

func userRowToSummary(row sqlcgen.User) in.UserSummary {
	summary := in.UserSummary{
		ID:            row.ID,
		Username:      row.Username,
		Role:          row.Role,
		Status:        row.Status,
		PasswordLogin: row.PasswordHash.Valid,
		OIDCLinked:    row.OidcIssuer.Valid,
		CreatedAt:     fromMillis(row.CreatedAt),
	}
	if row.OidcIssuer.Valid {
		summary.OIDCIdentity = &in.OIDCIdentity{Issuer: row.OidcIssuer.String, Subject: row.OidcSubject.String}
	}
	return summary
}

func marshalSession(s *session.Session) sqlcgen.InsertSessionParams {
	return sqlcgen.InsertSessionParams{
		ID:                    s.ID().String(),
		UserID:                s.UserID(),
		TokenHash:             s.TokenHash().String(),
		UserSessionGeneration: int64(s.UserSessionGeneration()),
		CreatedAt:             toMillis(s.CreatedAt()),
		LastSeenAt:            toMillis(s.LastSeenAt()),
		AbsoluteExpiresAt:     toMillis(s.AbsoluteExpiresAt()),
		IdleExpiresAt:         toMillis(s.IdleExpiresAt()),
		RevokedAt:             toNullMillis(s.RevokedAt()),
	}
}

func unmarshalSession(row sqlcgen.Session) (*session.Session, error) {
	id, err := session.NewID(row.ID)
	if err != nil {
		return nil, err
	}
	tokenHash, err := session.NewTokenHashFromString(row.TokenHash)
	if err != nil {
		return nil, err
	}
	return session.UnmarshalSessionFromDatabase(id, row.UserID, tokenHash, int(row.UserSessionGeneration),
		fromMillis(row.CreatedAt), fromMillis(row.LastSeenAt), fromMillis(row.AbsoluteExpiresAt),
		fromMillis(row.IdleExpiresAt), fromNullMillis(row.RevokedAt))
}
