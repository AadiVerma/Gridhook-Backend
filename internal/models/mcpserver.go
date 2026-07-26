package models

import "time"

type MCPServerStatus string

const (
	ServerRunning MCPServerStatus = "running"
	ServerStopped MCPServerStatus = "stopped"
)

type MCPServer struct {
	ID                 int64           `json:"id" gorm:"column:id;primaryKey"`
	OrganizationID     int64           `json:"organizationId" gorm:"column:organization_id"`
	Name               string          `json:"name" gorm:"column:name"`
	Slug               string          `json:"slug" gorm:"column:slug"`
	Description        string          `json:"description" gorm:"column:description"`
	CustomInstructions string          `json:"customInstructions" gorm:"column:custom_instructions"`
	Status             MCPServerStatus `json:"status" gorm:"column:status"`
	CreatedAt          time.Time       `json:"createdAt" gorm:"column:created_at"`
	UpdatedAt          time.Time       `json:"updatedAt" gorm:"column:updated_at"`

	ConnectedClients int     `json:"connectedClients" gorm:"-"`
	Endpoint         string  `json:"endpoint" gorm:"-"`
	ConnectorIDs     []int64 `json:"connectorIds" gorm:"-"`
	ToolGroupIDs     []int64 `json:"toolGroupIds" gorm:"-"`
	APIKeyCount      int     `json:"apiKeyCount" gorm:"-"`
}

func (MCPServer) TableName() string { return "mcp_servers" }

type MCPServerAPIKey struct {
	ID          int64      `json:"id" gorm:"column:id;primaryKey"`
	MCPServerID int64      `json:"mcpServerId" gorm:"column:mcp_server_id"`
	Label       string     `json:"label" gorm:"column:label"`
	KeyPrefix   string     `json:"keyPrefix" gorm:"column:key_prefix"`
	KeyHash     string     `json:"-" gorm:"column:key_hash"`
	CreatedAt   time.Time  `json:"createdAt" gorm:"column:created_at"`
	RevokedAt   *time.Time `json:"revokedAt,omitempty" gorm:"column:revoked_at"`
}

func (MCPServerAPIKey) TableName() string { return "mcp_server_api_keys" }

type MCPServerToolGroup struct {
	MCPServerID int64     `gorm:"column:mcp_server_id;primaryKey"`
	ToolGroupID int64     `gorm:"column:tool_group_id;primaryKey"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

func (MCPServerToolGroup) TableName() string { return "mcp_server_tool_groups" }
