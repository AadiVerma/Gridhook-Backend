package schemes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"gridhook.dev/connector-backend/internal/httpx"
	"gridhook.dev/connector-backend/internal/models"
)

type OAuth2Scheme struct {
	client *httpx.Client
}

func NewOAuth2Scheme(client *httpx.Client) *OAuth2Scheme {
	return &OAuth2Scheme{client: client}
}

var _ Scheme = (*OAuth2Scheme)(nil)

const (
	clientAuthPost  = "post"
	clientAuthBasic = "basic"
)

type oauth2TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

type oauth2ErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func (s *OAuth2Scheme) Resolve(ctx context.Context, creds *models.ConnectorCredentials) (Credentials, error) {
	if creds.TokenURL == "" || creds.ClientID == "" {
		return Credentials{}, fmt.Errorf("%w: oauth2 needs token_url and client_id", ErrIncompleteCredentials)
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	if scope := metaString(creds.MetaData, "scope"); scope != "" {
		form.Set("scope", scope)
	}
	if audience := metaString(creds.MetaData, "audience"); audience != "" {
		form.Set("audience", audience)
	}

	authMethod := strings.ToLower(metaString(creds.MetaData, "clientAuth"))
	if authMethod != clientAuthBasic {
		authMethod = clientAuthPost
	}
	if authMethod == clientAuthPost {
		form.Set("client_id", creds.ClientID)
		form.Set("client_secret", creds.ClientSecret)
	}

	req, err := httpx.NewRequest(ctx, http.MethodPost, creds.TokenURL, []byte(form.Encode()))
	if err != nil {
		return Credentials{}, fmt.Errorf("schemes: oauth2 build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if authMethod == clientAuthBasic {

		req.SetBasicAuth(url.QueryEscape(creds.ClientID), url.QueryEscape(creds.ClientSecret))
	}

	resp, err := s.client.Do(ctx, req)
	if err != nil {
		return Credentials{}, fmt.Errorf("schemes: oauth2 token request: %w", err)
	}

	if resp.StatusCode >= 300 {
		return Credentials{}, fmt.Errorf("schemes: oauth2 token endpoint returned %d: %s",
			resp.StatusCode, describeOAuth2Error(resp.Body))
	}

	var tok oauth2TokenResponse
	if err := json.Unmarshal(resp.Body, &tok); err != nil {
		return Credentials{}, fmt.Errorf("schemes: oauth2 decode token response: %w", err)
	}
	if tok.AccessToken == "" {
		return Credentials{}, fmt.Errorf("schemes: oauth2 token response missing access_token")
	}

	tokenType := tok.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}

	return Credentials{
		Headers:          map[string]string{"Authorization": tokenType + " " + tok.AccessToken},
		ExpiresInSeconds: tok.ExpiresIn,
	}, nil
}

func describeOAuth2Error(body []byte) string {
	var parsed oauth2ErrorResponse
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Error == "" {
		return "unrecognized error response"
	}
	if parsed.ErrorDescription == "" {
		return parsed.Error
	}
	return parsed.Error + ": " + parsed.ErrorDescription
}

func metaString(meta models.JSONMap, key string) string {
	v, _ := meta[key].(string)
	return strings.TrimSpace(v)
}
