package models

import "time"

type AdapterCategory string

const (
	CategoryCRM            AdapterCategory = "crm"
	CategoryDevTools       AdapterCategory = "dev_tools"
	CategoryPayments       AdapterCategory = "payments"
	CategoryCommunications AdapterCategory = "communications"
	CategoryDatabase       AdapterCategory = "database"
	CategoryProductivity   AdapterCategory = "productivity"
	CategoryERP            AdapterCategory = "erp"
	CategorySupport        AdapterCategory = "support"
	CategoryHR             AdapterCategory = "hr"
	CategoryCommerce       AdapterCategory = "commerce"
)

// AdapterTemplate is a global, admin-curated catalog entry — a bundled spec
// an org can install into its own Connector/ConnectorAPI/MCPTool rows. It is
// never org-scoped; installing clones it into an org, it doesn't reference one.
type AdapterTemplate struct {
	ID           int64           `json:"id" gorm:"column:id;primaryKey"`
	Key          string          `json:"key" gorm:"column:key"`
	Name         string          `json:"name" gorm:"column:name"`
	Glyph        string          `json:"glyph,omitempty" gorm:"column:glyph"`
	Category     AdapterCategory `json:"category" gorm:"column:category"`
	Description  string          `json:"description" gorm:"column:description"`
	EngineType   EngineType      `json:"engineType" gorm:"column:engine_type"`
	AuthType     AuthType        `json:"authType" gorm:"column:auth_type"`
	BaseURL      string          `json:"baseUrl" gorm:"column:base_url"`
	SpecFormat   string          `json:"-" gorm:"column:spec_format"`
	SpecRaw      []byte          `json:"-" gorm:"column:spec_raw"`
	ToolCount    int             `json:"toolCount" gorm:"column:tool_count"`
	InstallCount int64           `json:"installCount" gorm:"column:install_count"`
	CreatedAt    time.Time       `json:"createdAt" gorm:"column:created_at"`
	UpdatedAt    time.Time       `json:"updatedAt" gorm:"column:updated_at"`
}

func (AdapterTemplate) TableName() string { return "adapter_templates" }
