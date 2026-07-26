package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"gridhook.dev/connector-backend/internal/models"
)

var (
	ErrEmailTaken         = errors.New("identity: email already registered")
	ErrInvalidCredentials = errors.New("identity: invalid email or password")
	ErrNotAMember         = errors.New("identity: not a member of that organization")
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

// OrgMembership summarizes one organization an email belongs to — used to
// disambiguate Login when an email belongs to more than one.
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

// Register always creates a brand-new organization. An email that already
// has a row in any organization is rejected outright — the only way to join
// an existing organization is an invite link from someone already in it
// (built later; that invite path is what will give one email rows in
// multiple organizations, chosen between at login).
func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*AuthResult, error) {
	var existing int64
	if err := s.db.WithContext(ctx).Model(&models.User{}).Where("email = ?", in.Email).Count(&existing).Error; err != nil {
		return nil, fmt.Errorf("identity: register: check email: %w", err)
	}
	if existing > 0 {
		return nil, ErrEmailTaken
	}

	passwordHash, err := HashPassword(in.Password)
	if err != nil {
		return nil, fmt.Errorf("identity: register: hash password: %w", err)
	}

	domain := emailDomain(in.Email)
	orgName := in.Organization
	if orgName == "" {
		orgName = domain
	}
	slug, err := newSlug(orgName)
	if err != nil {
		return nil, fmt.Errorf("identity: register: slug: %w", err)
	}

	u := &models.User{}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tenant := models.Tenant{Name: domain, Domain: domain}
		if err := tx.Create(&tenant).Error; err != nil {
			return fmt.Errorf("identity: register: create tenant: %w", err)
		}

		company := models.Company{Name: orgName}
		if err := tx.Create(&company).Error; err != nil {
			return fmt.Errorf("identity: register: create company: %w", err)
		}

		org := models.Organization{CompanyID: company.ID, TenantID: tenant.ID, Name: orgName, Slug: slug}
		if err := tx.Create(&org).Error; err != nil {
			return fmt.Errorf("identity: register: create organization: %w", err)
		}

		*u = models.User{
			OrganizationID: org.ID, Email: in.Email, Name: in.Name, PasswordHash: passwordHash,
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

// Login verifies the password once, then resolves which organization to
// sign into:
//   - organizationID == 0 and exactly one membership: signs into it.
//   - organizationID == 0 and multiple memberships: returns them (nil
//     AuthResult, nil error) so the caller can ask the user to choose.
//   - organizationID != 0: signs into that membership, or ErrNotAMember if
//     the email isn't part of it.
//
// All rows sharing an email are expected to carry the same password_hash —
// that invariant is what makes "one password, pick your org" work without a
// separate accounts table. The invite flow (built later) must copy the
// existing password_hash when adding someone to another organization,
// rather than generating an independent one.
func (s *AuthService) Login(ctx context.Context, email, password string, organizationID int64) (*AuthResult, []OrgMembership, error) {
	memberships, err := s.membershipsForEmail(ctx, email)
	if err != nil {
		return nil, nil, fmt.Errorf("identity: login: list memberships: %w", err)
	}
	if len(memberships) == 0 {
		return nil, nil, ErrInvalidCredentials
	}

	var row struct {
		PasswordHash string `gorm:"column:password_hash"`
	}
	if err := s.db.WithContext(ctx).Model(&models.User{}).
		Select("password_hash").Where("email = ?", email).Take(&row).Error; err != nil {
		return nil, nil, fmt.Errorf("identity: login: %w", err)
	}
	if !VerifyPassword(row.PasswordHash, password) {
		return nil, nil, ErrInvalidCredentials
	}

	var chosen *OrgMembership
	switch {
	case organizationID != 0:
		for i := range memberships {
			if memberships[i].OrganizationID == organizationID {
				chosen = &memberships[i]
				break
			}
		}
		if chosen == nil {
			return nil, nil, ErrNotAMember
		}
	case len(memberships) == 1:
		chosen = &memberships[0]
	default:
		return nil, memberships, nil
	}

	if err := s.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", chosen.MembershipID).
		Update("last_active_at", gorm.Expr("now()")).Error; err != nil {
		return nil, nil, fmt.Errorf("identity: login: update last_active: %w", err)
	}

	now := time.Now()
	u := &models.User{
		ID: chosen.MembershipID, OrganizationID: chosen.OrganizationID, Email: email, Name: chosen.Name,
		Role: chosen.Role, Status: chosen.Status, LastActiveAt: &now, CreatedAt: chosen.CreatedAt,
	}
	sess, err := s.sessions.Issue(ctx, u.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("identity: login: issue session: %w", err)
	}
	return &AuthResult{User: u, Session: sess}, nil, nil
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

func newSlug(name string) (string, error) {
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return "", err
	}
	return slugify(name) + "-" + hex.EncodeToString(suffix), nil
}
