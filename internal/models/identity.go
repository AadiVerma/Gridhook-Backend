// Package models holds the shared structs mirroring the SQL schema in
// package can import it without pulling in the whole stack.
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
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

type Tenant struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Domain    string    `json:"domain"`
	CreatedAt time.Time `json:"createdAt"`
}

type Organization struct {
	ID        int64     `json:"id"`
	CompanyID int64     `json:"companyId"`
	TenantID  int64     `json:"tenantId"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Timezone  string    `json:"timezone"`
	CreatedAt time.Time `json:"createdAt"`
}

type User struct {
	ID             int64      `json:"id"`
	OrganizationID int64      `json:"organizationId"`
	Email          string     `json:"email"`
	Name           string     `json:"name"`
	PasswordHash   string     `json:"-"`
	Role           UserRole   `json:"role"`
	Status         UserStatus `json:"status"`
	LastActiveAt   *time.Time `json:"lastActive,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}


type Session struct {
	ID          int64      `json:"id"`
	UserID      int64      `json:"userId"`
	AccessToken string     `json:"-"`
	ExpiresAt   time.Time  `json:"expiresAt"`
	RevokedAt   *time.Time `json:"revokedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}
