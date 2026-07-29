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
	ID             int64           `json:"id" gorm:"column:id;primaryKey"`
	OrganizationID int64           `json:"organizationId" gorm:"column:organization_id"`
	Name           string          `json:"name" gorm:"column:name"`
	Glyph          string          `json:"glyph,omitempty" gorm:"column:glyph"`
	Description    string          `json:"description" gorm:"column:description"`
	Status         ConnectorStatus `json:"status" gorm:"column:status"`
	LastSyncAt     *time.Time      `json:"lastSync,omitempty" gorm:"column:last_sync_at"`
	CreatedAt      time.Time       `json:"createdAt" gorm:"column:created_at"`
	UpdatedAt      time.Time       `json:"updatedAt" gorm:"column:updated_at"`

	PrimaryType     EngineType `json:"type,omitempty" gorm:"-"`
	PrimaryBaseURL  string     `json:"baseUrl,omitempty" gorm:"-"`
	PrimaryAuthType AuthType   `json:"authType,omitempty" gorm:"-"`

	EngineTypes []EngineType `json:"engineTypes" gorm:"-"`
	AuthTypes   []AuthType   `json:"authTypes" gorm:"-"`
	APICount    int          `json:"apiCount" gorm:"-"`
	ToolCount   int          `json:"toolCount" gorm:"-"`
	ModuleCount int          `json:"moduleCount" gorm:"-"`
}

func (Connector) TableName() string { return "connectors" }

type ConnectorAPI struct {
	ID          int64      `json:"id" gorm:"column:id;primaryKey"`
	ConnectorID int64      `json:"connectorId" gorm:"column:connector_id"`
	GroupID     *int64     `json:"groupId" gorm:"column:group_id"`
	Name        string     `json:"name" gorm:"column:name"`
	EngineType  EngineType `json:"engineType" gorm:"column:engine_type"`
	BaseURL     string     `json:"baseUrl" gorm:"column:base_url"`
	AuthType    AuthType   `json:"authType" gorm:"column:auth_type"`
	SpecURL     string     `json:"specUrl,omitempty" gorm:"column:spec_url"`
	SpecRaw     []byte     `json:"specRaw,omitempty" gorm:"column:spec_raw"`
	IsActive    bool       `json:"isActive" gorm:"column:is_active"`
	CreatedAt   time.Time  `json:"createdAt" gorm:"column:created_at"`
	UpdatedAt   time.Time  `json:"updatedAt" gorm:"column:updated_at"`
}

func (ConnectorAPI) TableName() string { return "connector_apis" }

type ConnectorCredentials struct {
	ID             int64    `json:"-" gorm:"column:id;primaryKey"`
	ConnectorAPIID int64    `json:"-" gorm:"column:connector_api_id"`
	AuthType       AuthType `json:"-" gorm:"column:auth_type"`
	TokenURL       string   `json:"-" gorm:"column:token_url"`
	ClientID       string   `json:"-" gorm:"column:client_id"`
	ClientSecret   string   `json:"-" gorm:"column:client_secret"`
	BearerToken    string   `json:"-" gorm:"column:bearer_token"`
	APIKeyHeader   string   `json:"-" gorm:"column:api_key_header"`
	APIKeyValue    string   `json:"-" gorm:"column:api_key_value"`
	BasicUsername  string   `json:"-" gorm:"column:basic_username"`
	BasicPassword  string   `json:"-" gorm:"column:basic_password"`
	Headers        JSONMap  `json:"-" gorm:"column:headers"`
	MetaData       JSONMap  `json:"-" gorm:"column:meta_data"`
}

func (ConnectorCredentials) TableName() string { return "connector_credentials" }
