package schemes

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gridhook.dev/connector-backend/internal/httpx"
	"gridhook.dev/connector-backend/internal/models"
)

func TestBearerScheme(t *testing.T) {
	got, err := BearerScheme{}.Resolve(t.Context(), &models.ConnectorCredentials{BearerToken: "tok"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Headers["Authorization"] != "Bearer tok" {
		t.Errorf("Authorization = %q", got.Headers["Authorization"])
	}

	_, err = BearerScheme{}.Resolve(t.Context(), &models.ConnectorCredentials{})
	if !errors.Is(err, ErrIncompleteCredentials) {
		t.Errorf("Resolve with no token = %v, want ErrIncompleteCredentials", err)
	}
}

func TestAPIKeyScheme(t *testing.T) {
	t.Run("uses the configured header", func(t *testing.T) {
		got, err := APIKeyScheme{}.Resolve(t.Context(), &models.ConnectorCredentials{
			APIKeyHeader: "X-Vendor-Key", APIKeyValue: "abc",
		})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got.Headers["X-Vendor-Key"] != "abc" {
			t.Errorf("headers = %v", got.Headers)
		}
	})

	t.Run("defaults the header name", func(t *testing.T) {
		got, err := APIKeyScheme{}.Resolve(t.Context(), &models.ConnectorCredentials{APIKeyValue: "abc"})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got.Headers["X-API-Key"] != "abc" {
			t.Errorf("headers = %v, want the X-API-Key default", got.Headers)
		}
	})

	t.Run("requires a value", func(t *testing.T) {
		_, err := APIKeyScheme{}.Resolve(t.Context(), &models.ConnectorCredentials{APIKeyHeader: "X-Key"})
		if !errors.Is(err, ErrIncompleteCredentials) {
			t.Errorf("Resolve = %v, want ErrIncompleteCredentials", err)
		}
	})
}

func TestBasicScheme(t *testing.T) {
	got, err := BasicScheme{}.Resolve(t.Context(), &models.ConnectorCredentials{
		BasicUsername: "ada", BasicPassword: "hunter2",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	encoded := strings.TrimPrefix(got.Headers["Authorization"], "Basic ")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("Authorization was not valid base64: %v", err)
	}
	if string(decoded) != "ada:hunter2" {
		t.Errorf("decoded credentials = %q", decoded)
	}

	_, err = BasicScheme{}.Resolve(t.Context(), &models.ConnectorCredentials{BasicPassword: "x"})
	if !errors.Is(err, ErrIncompleteCredentials) {
		t.Errorf("Resolve with no username = %v, want ErrIncompleteCredentials", err)
	}
}

func TestCredentials_StringIsRedacted(t *testing.T) {
	creds := Credentials{Headers: map[string]string{"Authorization": "Bearer super-secret-token"}}

	if strings.Contains(creds.String(), "super-secret-token") {
		t.Errorf("Credentials.String() leaked the token: %s", creds.String())
	}
}

func testClient(t *testing.T) *httpx.Client {
	t.Helper()
	cfg := httpx.DefaultConfig()
	cfg.MaxRetries = 0
	cfg.AllowPrivateNetworks = true
	client, err := httpx.New(cfg)
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	return client
}

func TestOAuth2Scheme_ClientCredentialsInBody(t *testing.T) {
	var form url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-123","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer server.Close()

	got, err := NewOAuth2Scheme(testClient(t)).Resolve(t.Context(), &models.ConnectorCredentials{
		TokenURL: server.URL, ClientID: "cid", ClientSecret: "csecret",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if form.Get("grant_type") != "client_credentials" {
		t.Errorf("grant_type = %q", form.Get("grant_type"))
	}
	if form.Get("client_id") != "cid" || form.Get("client_secret") != "csecret" {
		t.Errorf("client credentials not sent in the body: %v", form)
	}
	if got.Headers["Authorization"] != "Bearer tok-123" {
		t.Errorf("Authorization = %q", got.Headers["Authorization"])
	}
	if got.ExpiresInSeconds != 3600 {
		t.Errorf("ExpiresInSeconds = %d, want 3600", got.ExpiresInSeconds)
	}
}

func TestOAuth2Scheme_ClientCredentialsAsBasicAuth(t *testing.T) {
	var (
		gotUser, gotPass string
		hadBasic         bool
		form             url.Values
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, hadBasic = r.BasicAuth()
		_ = r.ParseForm()
		form = r.PostForm
		_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":60}`))
	}))
	defer server.Close()

	_, err := NewOAuth2Scheme(testClient(t)).Resolve(t.Context(), &models.ConnectorCredentials{
		TokenURL: server.URL, ClientID: "cid", ClientSecret: "csecret",
		MetaData: models.JSONMap{"clientAuth": "basic"},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if !hadBasic {
		t.Fatal("no Basic authorization header was sent")
	}
	if gotUser != "cid" || gotPass != "csecret" {
		t.Errorf("basic auth = %q/%q, want cid/csecret", gotUser, gotPass)
	}
	if form.Get("client_secret") != "" {
		t.Error("client_secret was also sent in the body; it must be one or the other")
	}
}

func TestOAuth2Scheme_PassesScopeAndAudience(t *testing.T) {
	var form url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm
		_, _ = w.Write([]byte(`{"access_token":"tok"}`))
	}))
	defer server.Close()

	_, err := NewOAuth2Scheme(testClient(t)).Resolve(t.Context(), &models.ConnectorCredentials{
		TokenURL: server.URL, ClientID: "cid",
		MetaData: models.JSONMap{"scope": "read:orders write:orders", "audience": "https://api.example.com"},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if form.Get("scope") != "read:orders write:orders" {
		t.Errorf("scope = %q", form.Get("scope"))
	}
	if form.Get("audience") != "https://api.example.com" {
		t.Errorf("audience = %q", form.Get("audience"))
	}
}

func TestOAuth2Scheme_DefaultsTokenType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"tok"}`))
	}))
	defer server.Close()

	got, err := NewOAuth2Scheme(testClient(t)).Resolve(t.Context(), &models.ConnectorCredentials{
		TokenURL: server.URL, ClientID: "cid",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Headers["Authorization"] != "Bearer tok" {
		t.Errorf("Authorization = %q, want a Bearer default", got.Headers["Authorization"])
	}
}

func TestOAuth2Scheme_RequiresTokenURLAndClientID(t *testing.T) {
	scheme := NewOAuth2Scheme(testClient(t))

	for _, creds := range []*models.ConnectorCredentials{
		{ClientID: "cid"},
		{TokenURL: "https://example.com/token"},
		{},
	} {
		if _, err := scheme.Resolve(t.Context(), creds); !errors.Is(err, ErrIncompleteCredentials) {
			t.Errorf("Resolve(%+v) = %v, want ErrIncompleteCredentials", creds, err)
		}
	}
}

func TestOAuth2Scheme_SurfacesProviderErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"bad secret"}`))
	}))
	defer server.Close()

	_, err := NewOAuth2Scheme(testClient(t)).Resolve(t.Context(), &models.ConnectorCredentials{
		TokenURL: server.URL, ClientID: "cid",
	})
	if err == nil {
		t.Fatal("Resolve accepted a 400 from the token endpoint")
	}
	if !strings.Contains(err.Error(), "invalid_client") {
		t.Errorf("error = %v, want it to carry the provider's error code", err)
	}
}

func TestOAuth2Scheme_DoesNotEchoRawErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client","debug":{"received_secret":"csecret-LEAKED"}}`))
	}))
	defer server.Close()

	_, err := NewOAuth2Scheme(testClient(t)).Resolve(t.Context(), &models.ConnectorCredentials{
		TokenURL: server.URL, ClientID: "cid", ClientSecret: "csecret-LEAKED",
	})
	if err == nil {
		t.Fatal("Resolve accepted a 401")
	}
	if strings.Contains(err.Error(), "csecret-LEAKED") {
		t.Errorf("error echoed the provider's raw body, leaking the secret: %v", err)
	}
}

func TestOAuth2Scheme_RejectsResponseWithoutToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"expires_in":3600}`))
	}))
	defer server.Close()

	_, err := NewOAuth2Scheme(testClient(t)).Resolve(t.Context(), &models.ConnectorCredentials{
		TokenURL: server.URL, ClientID: "cid",
	})
	if err == nil {
		t.Error("Resolve accepted a token response with no access_token")
	}
}
