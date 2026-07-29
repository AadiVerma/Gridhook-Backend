package idcodec

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
)

var ErrInvalidToken = errors.New("idcodec: token is invalid or was issued under a different key")

const (
	idBytes  = 8
	tagBytes = 4

	TokenLength = 16
)

const feistelRounds = 4

type Codec struct {
	prfKey []byte
	macKey []byte
}

func New(keyMaterial string) (*Codec, error) {
	if keyMaterial == "" {
		return nil, errors.New("idcodec: key material must not be empty")
	}

	prfKey := sha256.Sum256([]byte("gridhook/idcodec/v2/prf\x00" + keyMaterial))
	macKey := sha256.Sum256([]byte("gridhook/idcodec/v2/mac\x00" + keyMaterial))

	return &Codec{prfKey: prfKey[:], macKey: macKey[:]}, nil
}

func (c *Codec) Encode(id int64) string {
	if id == 0 {
		return ""
	}

	var token [idBytes + tagBytes]byte

	binary.BigEndian.PutUint64(token[:idBytes], c.permute(uint64(id))) //nolint:gosec // exact 64-bit reinterpretation
	copy(token[idBytes:], c.tag(token[:idBytes]))

	return base64.RawURLEncoding.EncodeToString(token[:])
}

func (c *Codec) Decode(token string) (int64, error) {
	if token == "" {
		return 0, nil
	}
	if len(token) != TokenLength {
		return 0, ErrInvalidToken
	}

	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != idBytes+tagBytes {
		return 0, ErrInvalidToken
	}

	if subtle.ConstantTimeCompare(raw[idBytes:], c.tag(raw[:idBytes])) != 1 {
		return 0, ErrInvalidToken
	}

	id := int64(c.unpermute(binary.BigEndian.Uint64(raw[:idBytes]))) //nolint:gosec // exact 64-bit reinterpretation
	if id <= 0 {
		return 0, ErrInvalidToken
	}
	return id, nil
}

func Looks(s string) bool {
	if len(s) != TokenLength {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		isBase64URL := (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') ||
			(ch >= '0' && ch <= '9') || ch == '-' || ch == '_'
		if !isBase64URL {
			return false
		}
	}
	return true
}

func (c *Codec) permute(v uint64) uint64 {
	left, right := uint32(v>>32), uint32(v) //nolint:gosec // deliberate 32-bit halves

	for round := range feistelRounds {
		left, right = right, left^c.roundFunc(round, right)
	}
	return uint64(left)<<32 | uint64(right)
}

func (c *Codec) unpermute(v uint64) uint64 {
	left, right := uint32(v>>32), uint32(v) //nolint:gosec // deliberate 32-bit halves

	for round := feistelRounds - 1; round >= 0; round-- {
		left, right = right^c.roundFunc(round, left), left
	}
	return uint64(left)<<32 | uint64(right)
}

func (c *Codec) roundFunc(round int, half uint32) uint32 {
	var input [5]byte

	input[0] = byte(round) //nolint:gosec // bounded by feistelRounds
	binary.BigEndian.PutUint32(input[1:], half)

	mac := hmac.New(sha256.New, c.prfKey)
	mac.Write(input[:])
	return binary.BigEndian.Uint32(mac.Sum(nil)[:4])
}

func (c *Codec) tag(permuted []byte) []byte {
	mac := hmac.New(sha256.New, c.macKey)
	mac.Write(permuted)
	return mac.Sum(nil)[:tagBytes]
}
