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
	ID       int64     `json:"id"`
	Key      string    `json:"key"`
	Label    string    `json:"label"`
	SyncedAt time.Time `json:"syncedAt"`
}


type ToolGroup struct {
	ID              int64         `json:"id"`
	OrganizationID  int64         `json:"organizationId"`
	Name            string        `json:"name"`
	Slug            string        `json:"slug"`
	Description     string        `json:"description"`
	Kind            ToolGroupKind `json:"kind"`
	SyncedModuleKey string        `json:"syncedModuleKey,omitempty"`
	CreatedAt       time.Time     `json:"createdAt"`
	UpdatedAt       time.Time     `json:"updatedAt"`
	ToolCount       int           `json:"toolCount,omitempty"`
}


type MCPTool struct {
	ID                int64          `json:"id"`
	ConnectorAPIID    int64          `json:"connectorApiId"`
	GroupID           int64          `json:"groupId,omitempty"`
	EngineType        EngineType     `json:"engineType"`
	Name              string         `json:"name"`
	Method            HTTPMethod     `json:"method"`
	Path              string         `json:"path"`
	Description       string         `json:"description"`
	Parameters        map[string]any `json:"parameters"`
	EndpointMapping   map[string]any `json:"endpointMapping"`
	ResponseMapping   map[string]any `json:"responseMapping"`
	OutputSchema      map[string]any `json:"outputSchema"`
	Cached            bool           `json:"cached"`
	CacheTTLSeconds   int            `json:"cacheTtlSeconds"`
	Status            ToolStatus     `json:"status"`
	Version           string         `json:"version"`
	DisplayTitle      string         `json:"displayTitle,omitempty"`
	DisplayOnFrontend bool           `json:"displayOnFrontend"`
	DeprecatedAt      *time.Time     `json:"deprecatedAt,omitempty"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
}
