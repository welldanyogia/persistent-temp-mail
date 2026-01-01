package domain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Custom errors for domain operations
var (
	ErrDomainNotFound     = errors.New("domain not found")
	ErrDomainExists       = errors.New("domain already registered")
	ErrDomainLimitReached = errors.New("domain limit reached")
	ErrVerificationFailed = errors.New("DNS verification failed")
	ErrAccessDenied       = errors.New("access denied")
	ErrInvalidDomainName  = errors.New("invalid domain name format")
	ErrReservedDomain     = errors.New("domain is reserved")
)

// Domain represents a custom domain entity
type Domain struct {
	ID                uuid.UUID  `db:"id" json:"id"`
	UserID            uuid.UUID  `db:"user_id" json:"user_id"`
	DomainName        string     `db:"domain_name" json:"domain_name"`
	VerificationToken string     `db:"verification_token" json:"verification_token"`
	IsVerified        bool       `db:"is_verified" json:"is_verified"`
	VerifiedAt        *time.Time `db:"verified_at" json:"verified_at,omitempty"`
	SSLEnabled        bool       `db:"ssl_enabled" json:"ssl_enabled"`
	SSLExpiresAt      *time.Time `db:"ssl_expires_at" json:"ssl_expires_at,omitempty"`
	CreatedAt         time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt         time.Time  `db:"updated_at" json:"updated_at"`
	AliasCount        int        `db:"-" json:"alias_count"` // computed field, not in DB
}

// ListOptions contains options for listing domains
type ListOptions struct {
	Page   int    // Page number (1-based)
	Limit  int    // Items per page (default: 20, max: 100)
	Status string // Filter by status: "pending", "verified", or empty for all
}

// DefaultListOptions returns default list options
func DefaultListOptions() ListOptions {
	return ListOptions{
		Page:  1,
		Limit: 20,
	}
}

// DeleteResult contains information about deleted resources
type DeleteResult struct {
	DomainID           uuid.UUID `json:"domain_id"`
	AliasesDeleted     int       `json:"aliases_deleted"`
	EmailsDeleted      int       `json:"emails_deleted"`
	AttachmentsDeleted int       `json:"attachments_deleted"`
}

// Repository defines the interface for domain data access
type Repository interface {
	// Create creates a new domain
	Create(ctx context.Context, domain *Domain) error

	// GetByID retrieves a domain by its ID
	GetByID(ctx context.Context, id uuid.UUID) (*Domain, error)

	// GetByUserID retrieves all domains for a user with pagination
	GetByUserID(ctx context.Context, userID uuid.UUID, opts ListOptions) ([]Domain, int, error)

	// GetByDomainName retrieves a domain by its name
	GetByDomainName(ctx context.Context, name string) (*Domain, error)

	// Update updates an existing domain
	Update(ctx context.Context, domain *Domain) error

	// Delete deletes a domain and returns info about deleted resources
	Delete(ctx context.Context, id uuid.UUID) (*DeleteResult, error)

	// CountByUserID counts the number of domains owned by a user
	CountByUserID(ctx context.Context, userID uuid.UUID) (int, error)
}
