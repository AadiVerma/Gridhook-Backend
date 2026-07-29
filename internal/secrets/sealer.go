package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

var ErrInvalidCiphertext = errors.New("secrets: ciphertext invalid or written under a different key")

const KeySize = 32

type Sealer interface {
	Seal(plaintext string) (string, error)
	Open(ciphertext string) (string, error)
}

type AESSealer struct {
	gcm cipher.AEAD
}

var _ Sealer = (*AESSealer)(nil)

func NewAESSealer(key []byte) (*AESSealer, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("secrets: key must be %d bytes, got %d", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: new gcm: %w", err)
	}
	return &AESSealer{gcm: gcm}, nil
}

func DeriveKey(material string) []byte {
	sum := sha256.Sum256([]byte(material))
	return sum[:]
}

func (s *AESSealer) Seal(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("secrets: read nonce: %w", err)
	}
	sealed := s.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (s *AESSealer) Open(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", ErrInvalidCiphertext
	}
	nonceSize := s.gcm.NonceSize()
	if len(raw) < nonceSize {
		return "", ErrInvalidCiphertext
	}
	plaintext, err := s.gcm.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return "", ErrInvalidCiphertext
	}
	return string(plaintext), nil
}
