package models

import "time"

type HTTPMethod string

const (
	MethodGET    HTTPMethod = "GET"
	MethodPOST   HTTPMethod = "POST"
	MethodPUT    HTTPMethod = "PUT"
	MethodPATCH  HTTPMethod = "PATCH"
	MethodDELETE HTTPMethod = "DELETE"
)

type ToolStatus string

const (
	ToolActive     ToolStatus = "active"
	ToolDeprecated ToolStatus = "deprecated"
)

type ToolGroupKind string

const (
	GroupManual ToolGroupKind = "manual"
	GroupSynced ToolGroupKind = "synced"
)

type Module struct {
	ID       int64     `json:"id" gorm:"column:id;primaryKey"`
	Key      string    `json:"key" gorm:"column:key"`
	Label    string    `json:"label" gorm:"column:label"`
	SyncedAt time.Time `json:"syncedAt" gorm:"column:synced_at"`
}

func (Module) TableName() string { return "modules" }

type ToolGroup struct {
	ID              int64         `json:"id" gorm:"column:id;primaryKey"`
	OrganizationID  int64         `json:"organizationId" gorm:"column:organization_id"`
	Name            string        `json:"name" gorm:"column:name"`
	Slug            string        `json:"slug" gorm:"column:slug"`
	Description     string        `json:"description" gorm:"column:description"`
	Kind            ToolGroupKind `json:"kind" gorm:"column:kind"`
	SyncedModuleKey string        `json:"syncedModuleKey,omitempty" gorm:"column:synced_module_key"`
	CreatedAt       time.Time     `json:"createdAt" gorm:"column:created_at"`
	UpdatedAt       time.Time     `json:"updatedAt" gorm:"column:updated_at"`

	ToolCount int `json:"toolCount,omitempty" gorm:"-"`
}

func (ToolGroup) TableName() string { return "tool_groups" }

type MCPTool struct {
	ID                int64      `json:"id" gorm:"column:id;primaryKey"`
	ConnectorAPIID    int64      `json:"connectorApiId" gorm:"column:connector_api_id"`
	GroupID           int64      `json:"groupId,omitempty" gorm:"-"`
	EngineType        EngineType `json:"engineType" gorm:"column:engine_type"`
	Name              string     `json:"name" gorm:"column:name"`
	Method            HTTPMethod `json:"method" gorm:"column:method"`
	Path              string     `json:"path" gorm:"column:path"`
	Description       string     `json:"description" gorm:"column:description"`
	Parameters        JSONMap    `json:"parameters" gorm:"column:parameters"`
	EndpointMapping   JSONMap    `json:"endpointMapping" gorm:"column:endpoint_mapping"`
	ResponseMapping   JSONMap    `json:"responseMapping" gorm:"column:response_mapping"`
	OutputSchema      JSONMap    `json:"outputSchema" gorm:"column:output_schema"`
	Cached            bool       `json:"cached" gorm:"column:cached"`
	CacheTTLSeconds   int        `json:"cacheTtlSeconds" gorm:"column:cache_ttl_seconds"`
	Status            ToolStatus `json:"status" gorm:"column:status"`
	Version           string     `json:"version" gorm:"column:version"`
	DisplayTitle      string     `json:"displayTitle,omitempty" gorm:"column:display_title"`
	DisplayOnFrontend bool       `json:"displayOnFrontend" gorm:"column:display_on_frontend"`
	DeprecatedAt      *time.Time `json:"deprecatedAt,omitempty" gorm:"column:deprecated_at"`
	CreatedAt         time.Time  `json:"createdAt" gorm:"column:created_at"`
	UpdatedAt         time.Time  `json:"updatedAt" gorm:"column:updated_at"`
}

func (MCPTool) TableName() string { return "mcp_tools" }
