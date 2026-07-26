package db

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// Sealer is the envelope-encryption boundary for connector_credentials'
// secret columns (client_secret, bearer_token, api_key_value, basic_password).
// Deliberately not applied to sessions.access_token (looked up by exact
// match, not decrypted) or users.password_hash (a one-way hash, never
// decrypted) — three genuinely different "keep it secret" mechanisms.
//
// The AES-GCM implementation below is a local-development default. Swap it
// for a KMS-backed implementation (AWS KMS Encrypt/Decrypt, GCP Secret
// Manager) in production by implementing the same interface.
type Sealer interface {
	Seal(plaintext string) (string, error)
	Open(ciphertext string) (string, error)
}

type AESSealer struct {
	gcm cipher.AEAD
}

// NewAESSealer builds a Sealer from a 32-byte key (AES-256). Callers get the
// key from a secrets manager at process start, never from a literal.
func NewAESSealer(key []byte) (*AESSealer, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("sealer: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("sealer: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("sealer: %w", err)
	}
	return &AESSealer{gcm: gcm}, nil
}

func (s *AESSealer) Seal(plaintext string) (string, error) {
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("sealer: nonce: %w", err)
	}
	ciphertext := s.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (s *AESSealer) Open(ciphertext string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("sealer: decode: %w", err)
	}
	nonceSize := s.gcm.NonceSize()
	if len(raw) < nonceSize {
		return "", fmt.Errorf("sealer: ciphertext too short")
	}
	nonce, data := raw[:nonceSize], raw[nonceSize:]
	plaintext, err := s.gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return "", fmt.Errorf("sealer: open: %w", err)
	}
	return string(plaintext), nil
}
