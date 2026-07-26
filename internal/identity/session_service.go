package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"gorm.io/gorm"

	"gridhook.dev/connector-backend/internal/models"
)

var ErrSessionInvalid = errors.New("identity: session invalid or expired")

type SessionService struct {
	db  *gorm.DB
	ttl time.Duration
}

func NewSessionService(gdb *gorm.DB, ttl time.Duration) *SessionService {
	return &SessionService{db: gdb, ttl: ttl}
}

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
	if err := s.db.WithContext(ctx).Create(sess).Error; err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *SessionService) Verify(ctx context.Context, token string) (*models.User, error) {
	u := &models.User{}
	err := s.db.WithContext(ctx).
		Joins("JOIN sessions ON sessions.user_id = users.id").
		Where("sessions.access_token = ?", token).
		Where("sessions.revoked_at IS NULL").
		Where("sessions.expires_at > now()").
		First(u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSessionInvalid
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *SessionService) Revoke(ctx context.Context, token string) error {
	return s.db.WithContext(ctx).Model(&models.Session{}).
		Where("access_token = ? AND revoked_at IS NULL", token).
		Update("revoked_at", gorm.Expr("now()")).Error
}

func (s *SessionService) SweepExpired(ctx context.Context, olderThan time.Duration) (int64, error) {
	tx := s.db.WithContext(ctx).
		Where("expires_at < now() - make_interval(secs => ?)", olderThan.Seconds()).
		Delete(&models.Session{})
	return tx.RowsAffected, tx.Error
}

func randomToken(nBytes int) (string, error) {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
