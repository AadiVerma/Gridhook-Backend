package identity

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

const MinPasswordLength = 8

const maxPasswordLength = 72

var (
	ErrPasswordTooShort = fmt.Errorf("identity: password must be at least %d characters", MinPasswordLength)
	ErrPasswordTooLong  = fmt.Errorf("identity: password must be at most %d bytes", maxPasswordLength)
)

func HashPassword(plaintext string) (string, error) {
	switch {
	case len(plaintext) < MinPasswordLength:
		return "", ErrPasswordTooShort
	case len(plaintext) > maxPasswordLength:
		return "", ErrPasswordTooLong
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("identity: hash password: %w", err)
	}
	return string(hash), nil
}

func VerifyPassword(hash, plaintext string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) == nil
}

var decoyHash = sync.OnceValue(func() []byte {
	hash, err := bcrypt.GenerateFromPassword([]byte("gridhook-constant-time-decoy"), bcryptCost)
	if err != nil {

		return nil
	}
	return hash
})

func BurnPasswordComparison(plaintext string) {
	_ = bcrypt.CompareHashAndPassword(decoyHash(), []byte(plaintext))
}

func IsPasswordPolicyError(err error) bool {
	return errors.Is(err, ErrPasswordTooShort) || errors.Is(err, ErrPasswordTooLong)
}
