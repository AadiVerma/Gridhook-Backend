package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"gridhook.dev/connector-backend/internal/models"
)

var (
	ErrEmailTaken         = errors.New("identity: email already registered")
	ErrInvalidCredentials = errors.New("identity: invalid email or password")
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

func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*AuthResult, error) {
	var existing int64
	if err := s.db.WithContext(ctx).Model(&models.User{}).Where("email = ?", in.Email).Count(&existing).Error; err != nil {
		return nil, fmt.Errorf("identity: register: check email: %w", err)
	}
	if existing > 0 {
		return nil, ErrEmailTaken
	}

	domain := emailDomain(in.Email)
	passwordHash, err := HashPassword(in.Password)
	if err != nil {
		return nil, fmt.Errorf("identity: register: hash password: %w", err)
	}

	u := &models.User{}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tenant models.Tenant
		err := tx.Where("domain = ?", domain).First(&tenant).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			tenant = models.Tenant{Name: domain, Domain: domain}
			if err := tx.Create(&tenant).Error; err != nil {
				return fmt.Errorf("identity: register: create tenant: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("identity: register: resolve tenant: %w", err)
		}

		var isNewOrg bool
		var org models.Organization
		err = tx.Where("tenant_id = ?", tenant.ID).First(&org).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			isNewOrg = true
			orgName := in.Organization
			if orgName == "" {
				orgName = domain
			}
			company := models.Company{Name: orgName}
			if err := tx.Create(&company).Error; err != nil {
				return fmt.Errorf("identity: register: create company: %w", err)
			}
			org = models.Organization{CompanyID: company.ID, TenantID: tenant.ID, Name: orgName, Slug: slugify(orgName)}
			if err := tx.Create(&org).Error; err != nil {
				return fmt.Errorf("identity: register: create organization: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("identity: register: resolve organization: %w", err)
		}

		role := models.RoleDeveloper
		status := models.UserStatusActive
		if isNewOrg {
			role = models.RoleOwner
		}

		*u = models.User{OrganizationID: org.ID, Email: in.Email, Name: in.Name, PasswordHash: passwordHash, Role: role, Status: status}
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

func (s *AuthService) Login(ctx context.Context, email, password string) (*AuthResult, error) {
	u := &models.User{}
	err := s.db.WithContext(ctx).Where("email = ?", email).First(u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("identity: login: %w", err)
	}
	if !VerifyPassword(u.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}

	if err := s.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", u.ID).
		Update("last_active_at", gorm.Expr("now()")).Error; err != nil {
		return nil, fmt.Errorf("identity: login: update last_active: %w", err)
	}

	sess, err := s.sessions.Issue(ctx, u.ID)
	if err != nil {
		return nil, fmt.Errorf("identity: login: issue session: %w", err)
	}
	return &AuthResult{User: u, Session: sess}, nil
}

func emailDomain(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return email
	}
	return strings.ToLower(parts[1])
}

func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "-")
	return s
}
