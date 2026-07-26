package models

import "time"

type ConnectorStatus string

const (
	ConnectorActive   ConnectorStatus = "active"
	ConnectorInactive ConnectorStatus = "inactive"
	ConnectorError    ConnectorStatus = "error"
)

type EngineType string

const (
	EngineREST    EngineType = "REST"
	EngineSOAP    EngineType = "SOAP"
	EngineGraphQL EngineType = "GRAPHQL"
)

type AuthType string

const (
	AuthOAuth2     AuthType = "oauth2"
	AuthBearer     AuthType = "bearer"
	AuthAPIKey     AuthType = "api_key"
	AuthBasic      AuthType = "basic"
	AuthLoginToken AuthType = "login_token"
	AuthNone       AuthType = "none"
)

type Connector struct {
	ID             int64           `json:"id"`
	OrganizationID int64           `json:"organizationId"`
	Name           string          `json:"name"`
	Glyph          string          `json:"glyph,omitempty"`
	Description    string          `json:"description"`
	Status         ConnectorStatus `json:"status"`
	LastSyncAt     *time.Time      `json:"lastSync,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`

	PrimaryType     EngineType `json:"type,omitempty"`
	PrimaryBaseURL  string     `json:"baseUrl,omitempty"`
	PrimaryAuthType AuthType   `json:"authType,omitempty"`
}


type ConnectorAPI struct {
	ID          int64      `json:"id"`
	ConnectorID int64      `json:"connectorId"`
	Name        string     `json:"name"`
	EngineType  EngineType `json:"engineType"`
	BaseURL     string     `json:"baseUrl"`
	AuthType    AuthType   `json:"authType"`
	SpecURL     string     `json:"specUrl,omitempty"`
	SpecRaw     []byte     `json:"specRaw,omitempty"`
	IsActive    bool       `json:"isActive"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type ConnectorCredentials struct {
	ID             int64          `json:"-"`
	ConnectorAPIID int64          `json:"-"`
	AuthType       AuthType       `json:"-"`
	TokenURL       string         `json:"-"`
	ClientID       string         `json:"-"`
	ClientSecret   string         `json:"-"`
	BearerToken    string         `json:"-"`
	APIKeyHeader   string         `json:"-"`
	APIKeyValue    string         `json:"-"`
	BasicUsername  string         `json:"-"`
	BasicPassword  string         `json:"-"`
	Headers        map[string]any `json:"-"`
	MetaData       map[string]any `json:"-"`
}

type ConnectorUserMapping struct {
	ID             int64          `json:"id"`
	ConnectorAPIID int64          `json:"connectorApiId"`
	UserID         int64          `json:"userId"`
	MetaData       map[string]any `json:"metaData"`
	CreatedAt      time.Time      `json:"createdAt"`
}
