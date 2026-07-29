package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"gridhook.dev/connector-backend/internal/models"
	"gridhook.dev/connector-backend/internal/slug"
)

var (
	ErrEmailTaken         = errors.New("identity: email already registered")
	ErrInvalidCredentials = errors.New("identity: invalid email or password")
	ErrNotAMember         = errors.New("identity: not a member of that organization")
	ErrInvalidEmail       = errors.New("identity: email is not valid")
)

type AuthService struct {
	db       *gorm.DB
	sessions *SessionService
}

func NewAuthService(gdb *gorm.DB, sessions *SessionService) *AuthService {
	return &AuthService{db: gdb, sessions: sessions}
}

type RegisterInput struct {
	Name         string
	Email        string
	Password     string
	Organization string
}

type AuthResult struct {
	User    *models.User
	Session *models.Session
}

type OrgMembership struct {
	MembershipID     int64             `gorm:"column:membership_id"`
	OrganizationID   int64             `gorm:"column:organization_id"`
	OrganizationName string            `gorm:"column:organization_name"`
	OrganizationSlug string            `gorm:"column:organization_slug"`
	Name             string            `gorm:"column:name"`
	Role             models.UserRole   `gorm:"column:role"`
	Status           models.UserStatus `gorm:"column:status"`
	CreatedAt        time.Time         `gorm:"column:created_at"`
}

func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*AuthResult, error) {
	email, err := NormalizeEmail(in.Email)
	if err != nil {
		return nil, err
	}

	passwordHash, err := HashPassword(in.Password)
	if err != nil {
		return nil, err
	}

	domain := emailDomain(email)
	orgName := in.Organization
	if orgName == "" {
		orgName = domain
	}
	orgSlug, err := slug.MakeUnique(orgName)
	if err != nil {
		return nil, fmt.Errorf("identity: register: slug: %w", err)
	}

	u := &models.User{}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {

		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?)::bigint)`, email).Error; err != nil {
			return fmt.Errorf("identity: register: lock email: %w", err)
		}

		var existing int64
		if err := tx.Model(&models.User{}).Where("email = ?", email).Count(&existing).Error; err != nil {
			return fmt.Errorf("identity: register: check email: %w", err)
		}
		if existing > 0 {
			return ErrEmailTaken
		}

		tenant := models.Tenant{Name: domain, Domain: domain}
		if err := tx.Create(&tenant).Error; err != nil {
			return fmt.Errorf("identity: register: create tenant: %w", err)
		}

		company := models.Company{Name: orgName}
		if err := tx.Create(&company).Error; err != nil {
			return fmt.Errorf("identity: register: create company: %w", err)
		}

		org := models.Organization{CompanyID: company.ID, TenantID: tenant.ID, Name: orgName, Slug: orgSlug}
		if err := tx.Create(&org).Error; err != nil {
			return fmt.Errorf("identity: register: create organization: %w", err)
		}

		*u = models.User{
			OrganizationID: org.ID, Email: email, Name: in.Name, PasswordHash: passwordHash,
			Role: models.RoleOwner, Status: models.UserStatusActive,
		}
		if err := tx.Create(u).Error; err != nil {
			return fmt.Errorf("identity: register: create user: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sess, err := s.sessions.Issue(ctx, u.ID)
	if err != nil {
		return nil, fmt.Errorf("identity: register: issue session: %w", err)
	}
	return &AuthResult{User: u, Session: sess}, nil
}

func (s *AuthService) Login(ctx context.Context, email, password string, organizationID int64) (*AuthResult, []OrgMembership, error) {
	normalized, err := NormalizeEmail(email)
	if err != nil {

		BurnPasswordComparison(password)
		return nil, nil, ErrInvalidCredentials
	}

	memberships, err := s.membershipsForEmail(ctx, normalized)
	if err != nil {
		return nil, nil, fmt.Errorf("identity: login: list memberships: %w", err)
	}
	if len(memberships) == 0 {

		BurnPasswordComparison(password)
		return nil, nil, ErrInvalidCredentials
	}

	var row struct {
		PasswordHash string `gorm:"column:password_hash"`
	}
	if err := s.db.WithContext(ctx).Model(&models.User{}).
		Select("password_hash").Where("email = ?", normalized).Take(&row).Error; err != nil {
		return nil, nil, fmt.Errorf("identity: login: %w", err)
	}
	if !VerifyPassword(row.PasswordHash, password) {
		return nil, nil, ErrInvalidCredentials
	}

	chosen, err := chooseMembership(memberships, organizationID)
	if err != nil {
		return nil, nil, err
	}
	if chosen == nil {
		return nil, memberships, nil
	}

	if err := s.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", chosen.MembershipID).
		Update("last_active_at", gorm.Expr("now()")).Error; err != nil {
		return nil, nil, fmt.Errorf("identity: login: update last_active: %w", err)
	}

	now := time.Now()
	u := &models.User{
		ID: chosen.MembershipID, OrganizationID: chosen.OrganizationID, Email: normalized, Name: chosen.Name,
		Role: chosen.Role, Status: chosen.Status, LastActiveAt: &now, CreatedAt: chosen.CreatedAt,
	}
	sess, err := s.sessions.Issue(ctx, u.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("identity: login: issue session: %w", err)
	}
	return &AuthResult{User: u, Session: sess}, nil, nil
}

func chooseMembership(memberships []OrgMembership, organizationID int64) (*OrgMembership, error) {
	if organizationID != 0 {
		for i := range memberships {
			if memberships[i].OrganizationID == organizationID {
				return &memberships[i], nil
			}
		}
		return nil, ErrNotAMember
	}
	if len(memberships) == 1 {
		return &memberships[0], nil
	}
	return nil, nil
}

func (s *AuthService) membershipsForEmail(ctx context.Context, email string) ([]OrgMembership, error) {
	var rows []OrgMembership
	err := s.db.WithContext(ctx).
		Table("users u").
		Select("u.id AS membership_id, u.organization_id, o.name AS organization_name, o.slug AS organization_slug, u.name, u.role, u.status, u.created_at").
		Joins("JOIN organizations o ON o.id = u.organization_id").
		Where("u.email = ?", email).
		Order("o.name").
		Scan(&rows).Error
	return rows, err
}

func NormalizeEmail(email string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndex(normalized, "@")
	if at <= 0 || at == len(normalized)-1 || strings.ContainsAny(normalized, " \t\r\n") {
		return "", ErrInvalidEmail
	}
	return normalized, nil
}

func emailDomain(email string) string {
	_, domain, found := strings.Cut(email, "@")
	if !found {
		return email
	}
	return domain
}
