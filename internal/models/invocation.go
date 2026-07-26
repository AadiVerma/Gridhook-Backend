package models

import "time"

type InvocationStatus string

const (
	InvocationSuccess InvocationStatus = "success"
	InvocationError   InvocationStatus = "error"
	InvocationTimeout InvocationStatus = "timeout"
)

// ToolInvocation is the append-only audit row written at the end of every
// dispatch, whether triggered by a test-run (Phase 5's POST .../run) or real
// MCP client traffic. Denormalized so audit-log filters need no joins.
type ToolInvocation struct {
	ID             int64            `json:"id"`
	ToolID         int64            `json:"tool"`
	ConnectorID    int64            `json:"connector"`
	ConnectorAPIID int64            `json:"connectorApiId"`
	MCPServerID    int64            `json:"server,omitempty"`
	OrganizationID int64            `json:"-"`
	UserID         int64            `json:"-"`
	UserEmail      string           `json:"-"`
	Status         InvocationStatus `json:"status"`
	HTTPCode       int              `json:"code"`
	DurationMs     int              `json:"durationMs"`
	Input          map[string]any   `json:"input"`
	Output         map[string]any   `json:"output"`
	Error          string           `json:"error,omitempty"`
	CreatedAt      time.Time        `json:"time"`
}
