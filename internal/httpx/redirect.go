package httpx

import (
	"fmt"
	"net/http"
	"strings"
)

var credentialHeaders = []string{
	"Authorization",
	"Cookie",
	"Proxy-Authorization",
	"X-Api-Key",
	"X-Auth-Token",
	"X-Access-Token",
	"Api-Key",
	"Apikey",
	"X-Amz-Security-Token",
	"X-Csrf-Token",
}

func checkRedirect(maxRedirects int) func(*http.Request, []*http.Request) error {
	if maxRedirects <= 0 {
		return func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("httpx: stopped after %d redirects", maxRedirects)
		}
		previous := via[len(via)-1]
		if !sameHost(previous.URL.Host, req.URL.Host) {
			stripCredentialHeaders(req.Header)
		}
		return nil
	}
}

func sameHost(a, b string) bool {
	return strings.EqualFold(a, b)
}

func stripCredentialHeaders(h http.Header) {
	for _, name := range credentialHeaders {
		h.Del(name)
	}

	for _, name := range h.Values(HeaderSensitiveNames) {
		for candidate := range strings.SplitSeq(name, ",") {
			if candidate = strings.TrimSpace(candidate); candidate != "" {
				h.Del(candidate)
			}
		}
	}
	h.Del(HeaderSensitiveNames)
}

const HeaderSensitiveNames = "X-Gridhook-Sensitive-Headers"

func MarkSensitive(h http.Header, names ...string) {
	if len(names) == 0 {
		return
	}
	existing := h.Values(HeaderSensitiveNames)
	h.Set(HeaderSensitiveNames, strings.Join(append(existing, strings.Join(names, ",")), ","))
}
