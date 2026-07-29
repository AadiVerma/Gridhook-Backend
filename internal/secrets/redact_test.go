package secrets

import (
	"strings"
	"testing"
)

func TestSanitizeURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no credentials is unchanged",
			in:   "https://api.example.com/v1/users?page=2",
			want: "https://api.example.com/v1/users?page=2",
		},
		{
			name: "api_key value redacted",
			in:   "https://api.example.com/v1/users?api_key=live_abc123",
			want: "https://api.example.com/v1/users?api_key=%5BREDACTED%5D",
		},
		{
			name: "userinfo dropped entirely",
			in:   "https://user:hunter2@api.example.com/v1",
			want: "https://api.example.com/v1",
		},
		{
			name: "fragment dropped",
			in:   "https://api.example.com/v1#token=abc",
			want: "https://api.example.com/v1",
		},
		{
			name: "empty stays empty",
			in:   "",
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeURL(tc.in); got != tc.want {
				t.Errorf("SanitizeURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeURL_RedactsCredentialParamVariants(t *testing.T) {
	params := []string{
		"api_key", "apikey", "apiKey", "API_KEY",
		"access_token", "accessToken", "token", "auth_token",
		"client_secret", "secret", "password", "signature", "sig", "key",
	}

	const secret = "SUPERSECRET"
	for _, param := range params {
		t.Run(param, func(t *testing.T) {
			got := SanitizeURL("https://api.example.com/v1?" + param + "=" + secret)
			if strings.Contains(got, secret) {
				t.Errorf("SanitizeURL leaked the value of %q: %s", param, got)
			}
		})
	}
}

func TestSanitizeURL_KeepsBenignParams(t *testing.T) {
	got := SanitizeURL("https://api.example.com/v1?page=2&limit=50&token=abc")
	for _, want := range []string{"page=2", "limit=50"} {
		if !strings.Contains(got, want) {
			t.Errorf("SanitizeURL dropped benign param %q: %s", want, got)
		}
	}
	if strings.Contains(got, "abc") {
		t.Errorf("SanitizeURL leaked the token: %s", got)
	}
}

func TestSanitizeURL_UnparseableIsNotEchoed(t *testing.T) {
	got := SanitizeURL("https://exa mple.com/v1?token=leakme")
	if strings.Contains(got, "leakme") {
		t.Errorf("SanitizeURL echoed an unparseable URL's secret: %s", got)
	}
}
