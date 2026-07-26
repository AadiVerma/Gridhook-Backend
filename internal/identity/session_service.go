package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gridhook.dev/connector-backend/internal/models"
)

var ErrSessionInvalid = errors.New("identity: session invalid or expired")

// SessionService owns issue/verify/revoke for this platform's OWN identity —
// distinct from internal/auth, which resolves credentials for calling a
// connector's upstream API.
type SessionService struct {
	pool *pgxpool.Pool
	ttl  time.Duration
}

func NewSessionService(pool *pgxpool.Pool, ttl time.Duration) *SessionService {
	return &SessionService{pool: pool, ttl: ttl}
}

// Issue creates a fresh session row. Never reused or updated in place, so a
// user can be logged in from two places at once.
func (s *SessionService) Issue(ctx context.Context, userID int64) (*models.Session, error) {
	token, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	sess := &models.Session{
		UserID:      userID,
		AccessToken: token,
		ExpiresAt:   time.Now().Add(s.ttl),
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO sessions (user_id, access_token, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id, created_at
	`, sess.UserID, sess.AccessToken, sess.ExpiresAt).Scan(&sess.ID, &sess.CreatedAt)
	if err != nil {
		return nil, err
	}
	return sess, nil
}

// Verify resolves a bearer token to its owning user, or ErrSessionInvalid if
// the token is unknown, revoked, or expired.
func (s *SessionService) Verify(ctx context.Context, token string) (*models.User, error) {
	var u models.User
	err := s.pool.QueryRow(ctx, `
		SELECT u.id, u.organization_id, u.email, u.name, u.password_hash, u.role, u.status, u.last_active_at, u.created_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.access_token = $1
		  AND s.revoked_at IS NULL
		  AND s.expires_at > now()
	`, token).Scan(&u.ID, &u.OrganizationID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.Status, &u.LastActiveAt, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionInvalid
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *SessionService) Revoke(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE access_token = $1 AND revoked_at IS NULL`, token)
	return err
}

// SweepExpired deletes long-expired rows so the table doesn't grow
// unbounded. Intended to run periodically from cmd/worker.
func (s *SessionService) SweepExpired(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now() - make_interval(secs => $1)`, olderThan.Seconds())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func randomToken(nBytes int) (string, error) {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
