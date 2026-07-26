package models

import "time"

type InvocationStatus string

const (
	InvocationSuccess InvocationStatus = "success"
	InvocationError   InvocationStatus = "error"
	InvocationTimeout InvocationStatus = "timeout"
)

type ToolInvocation struct {
	ID             int64            `json:"id" gorm:"column:id"`
	ToolID         int64            `json:"tool" gorm:"column:tool_id"`
	ConnectorID    int64            `json:"connector" gorm:"column:connector_id"`
	ConnectorAPIID int64            `json:"connectorApiId" gorm:"column:connector_api_id"`
	MCPServerID    int64            `json:"server,omitempty" gorm:"column:mcp_server_id"`
	OrganizationID int64            `json:"-" gorm:"column:organization_id"`
	UserID         int64            `json:"-" gorm:"column:user_id"`
	UserEmail      string           `json:"-" gorm:"column:user_email"`
	Status         InvocationStatus `json:"status" gorm:"column:status"`
	HTTPCode       int              `json:"code" gorm:"column:http_code"`
	DurationMs     int              `json:"durationMs" gorm:"column:duration_ms"`
	Input          JSONMap          `json:"input" gorm:"column:input"`
	Output         JSONMap          `json:"output" gorm:"column:output"`
	Error          string           `json:"error,omitempty" gorm:"column:error"`
	CreatedAt      time.Time        `json:"time" gorm:"column:created_at"`
}

func (ToolInvocation) TableName() string { return "tool_invocations" }
