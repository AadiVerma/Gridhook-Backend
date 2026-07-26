package models

import "time"

type MCPServerStatus string

const (
	ServerRunning MCPServerStatus = "running"
	ServerStopped MCPServerStatus = "stopped"
)

type MCPServer struct {
	ID                 int64           `json:"id"`
	OrganizationID     int64           `json:"organizationId"`
	Name               string          `json:"name"`
	Slug               string          `json:"slug"`
	Description        string          `json:"description"`
	CustomInstructions string          `json:"customInstructions"`
	Status             MCPServerStatus `json:"status"`
	CreatedAt          time.Time       `json:"createdAt"`
	UpdatedAt          time.Time       `json:"updatedAt"`

	// Read-time aggregates.
	ConnectedClients int     `json:"connectedClients"`
	Endpoint         string  `json:"endpoint"`
	ConnectorIDs     []int64 `json:"connectorIds"`
	ToolGroupIDs     []int64 `json:"toolGroupIds"`
	APIKeyCount      int     `json:"apiKeyCount"`
}

type MCPServerAPIKey struct {
	ID          int64      `json:"id"`
	MCPServerID int64      `json:"mcpServerId"`
	Label       string     `json:"label"`
	KeyPrefix   string     `json:"keyPrefix"`
	KeyHash     string     `json:"-"`
	CreatedAt   time.Time  `json:"createdAt"`
	RevokedAt   *time.Time `json:"revokedAt,omitempty"`
}
