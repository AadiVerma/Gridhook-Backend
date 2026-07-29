package slug

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
)

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

const Fallback = "item"

func Make(name string) string {
	s := nonAlphanumeric.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	if s = strings.Trim(s, "-"); s == "" {
		return Fallback
	}
	return s
}

func MakeUnique(name string) (string, error) {
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return "", err
	}
	return Make(name) + "-" + hex.EncodeToString(suffix), nil
}
