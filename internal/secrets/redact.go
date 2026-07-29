package secrets

import (
	"net/url"
	"strings"
)

const Redacted = "[REDACTED]"

var sensitiveQueryKeys = []string{
	"access_token",
	"api_key",
	"apikey",
	"auth",
	"client_secret",
	"key",
	"password",
	"secret",
	"signature",
	"sig",
	"token",
}

func SanitizeURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		if scheme, rest, found := strings.Cut(raw, "://"); found {
			host, _, _ := strings.Cut(rest, "/")
			return scheme + "://" + host + "/" + Redacted
		}
		return Redacted
	}

	u.User = nil
	if q := u.Query(); len(q) > 0 {
		for key := range q {
			if isSensitiveKey(key) {
				q.Set(key, Redacted)
			}
		}
		u.RawQuery = q.Encode()
	}
	u.Fragment = ""
	return u.String()
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, candidate := range sensitiveQueryKeys {
		if strings.Contains(lower, candidate) {
			return true
		}
	}
	return false
}
