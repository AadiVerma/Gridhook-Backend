package models

type ResolvedTool struct {
	Tool *MCPTool
	API  *ConnectorAPI
}

func (r *ResolvedTool) ConnectorID() int64 {
	if r.API == nil {
		return 0
	}
	return r.API.ConnectorID
}
