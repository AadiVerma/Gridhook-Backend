package schemes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gridhook.dev/connector-backend/internal/models"
)

// OAuth2Scheme implements the client-credentials grant — the only grant a
// server-to-server connector needs (no user present to redirect through an
// authorization-code flow).
type OAuth2Scheme struct {
	HTTPClient *http.Client
}

func NewOAuth2Scheme() *OAuth2Scheme {
	return &OAuth2Scheme{HTTPClient: &http.Client{Timeout: 15 * time.Second}}
}

type oauth2TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

func (s *OAuth2Scheme) Resolve(ctx context.Context, creds *models.ConnectorCredentials) (Credentials, error) {
	if creds.TokenURL == "" || creds.ClientID == "" {
		return Credentials{}, fmt.Errorf("schemes: oauth2 credentials missing token_url/client_id")
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", creds.ClientID)
	form.Set("client_secret", creds.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, creds.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client().Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("schemes: oauth2 token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return Credentials{}, fmt.Errorf("schemes: oauth2 token endpoint returned %d", resp.StatusCode)
	}

	var tok oauth2TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return Credentials{}, fmt.Errorf("schemes: oauth2 decode token response: %w", err)
	}
	if tok.AccessToken == "" {
		return Credentials{}, fmt.Errorf("schemes: oauth2 token response missing access_token")
	}

	return Credentials{
		Headers:          map[string]string{"Authorization": "Bearer " + tok.AccessToken},
		ExpiresInSeconds: tok.ExpiresIn,
	}, nil
}

func (s *OAuth2Scheme) client() *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	return http.DefaultClient
}
