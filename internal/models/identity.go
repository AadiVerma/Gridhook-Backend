package models

import "time"

type UserRole string

const (
	RoleOwner     UserRole = "owner"
	RoleAdmin     UserRole = "admin"
	RoleDeveloper UserRole = "developer"
	RoleViewer    UserRole = "viewer"
)

type UserStatus string

const (
	UserStatusActive  UserStatus = "active"
	UserStatusInvited UserStatus = "invited"
)

type Company struct {
	ID        int64     `json:"id" gorm:"column:id;primaryKey"`
	Name      string    `json:"name" gorm:"column:name"`
	CreatedAt time.Time `json:"createdAt" gorm:"column:created_at"`
}

func (Company) TableName() string { return "companies" }

type Tenant struct {
	ID        int64     `json:"id" gorm:"column:id;primaryKey"`
	Name      string    `json:"name" gorm:"column:name"`
	Domain    string    `json:"domain" gorm:"column:domain"`
	CreatedAt time.Time `json:"createdAt" gorm:"column:created_at"`
}

func (Tenant) TableName() string { return "tenants" }

type Organization struct {
	ID        int64     `json:"id" gorm:"column:id;primaryKey"`
	CompanyID int64     `json:"companyId" gorm:"column:company_id"`
	TenantID  int64     `json:"tenantId" gorm:"column:tenant_id"`
	Name      string    `json:"name" gorm:"column:name"`
	Slug      string    `json:"slug" gorm:"column:slug"`
	Timezone  string    `json:"timezone" gorm:"column:timezone"`
	CreatedAt time.Time `json:"createdAt" gorm:"column:created_at"`
}

func (Organization) TableName() string { return "organizations" }

type User struct {
	ID             int64      `json:"id" gorm:"column:id;primaryKey"`
	OrganizationID int64      `json:"organizationId" gorm:"column:organization_id"`
	Email          string     `json:"email" gorm:"column:email"`
	Name           string     `json:"name" gorm:"column:name"`
	PasswordHash   string     `json:"-" gorm:"column:password_hash"`
	Role           UserRole   `json:"role" gorm:"column:role"`
	Status         UserStatus `json:"status" gorm:"column:status"`
	LastActiveAt   *time.Time `json:"lastActive,omitempty" gorm:"column:last_active_at"`
	CreatedAt      time.Time  `json:"createdAt" gorm:"column:created_at"`
}

func (User) TableName() string { return "users" }

type Session struct {
	ID          int64      `json:"id" gorm:"column:id;primaryKey"`
	UserID      int64      `json:"userId" gorm:"column:user_id"`
	AccessToken string     `json:"-" gorm:"column:access_token"`
	ExpiresAt   time.Time  `json:"expiresAt" gorm:"column:expires_at"`
	RevokedAt   *time.Time `json:"revokedAt,omitempty" gorm:"column:revoked_at"`
	CreatedAt   time.Time  `json:"createdAt" gorm:"column:created_at"`
}

func (Session) TableName() string { return "sessions" }
