package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gridhook.dev/connector-backend/internal/models"
)

var (
	ErrEmailTaken         = errors.New("identity: email already registered")
	ErrInvalidCredentials = errors.New("identity: invalid email or password")
)

type AuthService struct {
	pool     *pgxpool.Pool
	sessions *SessionService
}

func NewAuthService(pool *pgxpool.Pool, sessions *SessionService) *AuthService {
	return &AuthService{pool: pool, sessions: sessions}
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

// Register creates the account and its first organization (owner role). If
// another org already exists for this email's domain, the new user
// auto-joins it instead of creating a duplicate — the same behavior
// trd.md's tenant-domain matching describes.
func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*AuthResult, error) {
	var existing int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE email = $1`, in.Email).Scan(&existing); err != nil {
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

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("identity: register: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var tenantID int64
	err = tx.QueryRow(ctx, `SELECT id FROM tenants WHERE domain = $1`, domain).Scan(&tenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO tenants (name, domain) VALUES ($1,$2) RETURNING id`, domain, domain).Scan(&tenantID)
	}
	if err != nil {
		return nil, fmt.Errorf("identity: register: resolve tenant: %w", err)
	}

	var orgID int64
	var isNewOrg bool
	err = tx.QueryRow(ctx, `
		SELECT o.id FROM organizations o WHERE o.tenant_id = $1 LIMIT 1
	`, tenantID).Scan(&orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		isNewOrg = true
		var companyID int64
		orgName := in.Organization
		if orgName == "" {
			orgName = domain
		}
		if err := tx.QueryRow(ctx, `INSERT INTO companies (name) VALUES ($1) RETURNING id`, orgName).Scan(&companyID); err != nil {
			return nil, fmt.Errorf("identity: register: create company: %w", err)
		}
		slug := slugify(orgName)
		if err := tx.QueryRow(ctx, `
			INSERT INTO organizations (company_id, tenant_id, name, slug) VALUES ($1,$2,$3,$4) RETURNING id
		`, companyID, tenantID, orgName, slug).Scan(&orgID); err != nil {
			return nil, fmt.Errorf("identity: register: create organization: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("identity: register: resolve organization: %w", err)
	}

	role := models.RoleDeveloper
	status := models.UserStatusActive
	if isNewOrg {
		role = models.RoleOwner
	}

	u := &models.User{OrganizationID: orgID, Email: in.Email, Name: in.Name, Role: role, Status: status}
	err = tx.QueryRow(ctx, `
		INSERT INTO users (organization_id, email, name, password_hash, role, status)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, created_at
	`, orgID, in.Email, in.Name, passwordHash, role, status).Scan(&u.ID, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("identity: register: create user: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("identity: register: commit: %w", err)
	}

	sess, err := s.sessions.Issue(ctx, u.ID)
	if err != nil {
		return nil, fmt.Errorf("identity: register: issue session: %w", err)
	}
	return &AuthResult{User: u, Session: sess}, nil
}

func (s *AuthService) Login(ctx context.Context, email, password string) (*AuthResult, error) {
	u := &models.User{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, organization_id, email, name, password_hash, role, status, created_at
		FROM users WHERE email = $1
	`, email).Scan(&u.ID, &u.OrganizationID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.Status, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("identity: login: %w", err)
	}
	if !VerifyPassword(u.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}

	if _, err := s.pool.Exec(ctx, `UPDATE users SET last_active_at = now() WHERE id = $1`, u.ID); err != nil {
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
