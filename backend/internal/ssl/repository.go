// Package ssl provides SSL certificate management functionality
// Requirements: 2.4, 3.1, 3.6 - Certificate storage and renewal management
package ssl

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Custom errors for SSL certificate operations
var (
	ErrCertificateNotFound = errors.New("ssl certificate not found")
	ErrCertificateExists   = errors.New("ssl certificate already exists for this domain")
	ErrInvalidStatus       = errors.New("invalid certificate status")
)

// CertificateStatus represents the status of an SSL certificate
type CertificateStatus string

const (
	StatusPending      CertificateStatus = "pending"
	StatusProvisioning CertificateStatus = "provisioning"
	StatusActive       CertificateStatus = "active"
	StatusExpired      CertificateStatus = "expired"
	StatusRevoked      CertificateStatus = "revoked"
	StatusFailed       CertificateStatus = "failed"
)

// ValidStatuses contains all valid certificate statuses
var ValidStatuses = []CertificateStatus{
	StatusPending,
	StatusProvisioning,
	StatusActive,
	StatusExpired,
	StatusRevoked,
	StatusFailed,
}

// IsValid checks if the status is valid
func (s CertificateStatus) IsValid() bool {
	for _, valid := range ValidStatuses {
		if s == valid {
			return true
		}
	}
	return false
}


// SSLCertificate represents an SSL/TLS certificate in the database
// Requirements: 2.4 - Store certificate metadata in database
type SSLCertificate struct {
	ID                  uuid.UUID         `db:"id" json:"id"`
	DomainID            uuid.UUID         `db:"domain_id" json:"domain_id"`
	DomainName          string            `db:"domain_name" json:"domain_name"`
	Status              CertificateStatus `db:"status" json:"status"`
	Issuer              *string           `db:"issuer" json:"issuer,omitempty"`
	SerialNumber        *string           `db:"serial_number" json:"serial_number,omitempty"`
	IssuedAt            *time.Time        `db:"issued_at" json:"issued_at,omitempty"`
	ExpiresAt           *time.Time        `db:"expires_at" json:"expires_at,omitempty"`
	LastRenewalAttempt  *time.Time        `db:"last_renewal_attempt" json:"last_renewal_attempt,omitempty"`
	RenewalFailures     int               `db:"renewal_failures" json:"renewal_failures"`
	StoragePath         *string           `db:"storage_path" json:"storage_path,omitempty"`
	CreatedAt           time.Time         `db:"created_at" json:"created_at"`
	UpdatedAt           time.Time         `db:"updated_at" json:"updated_at"`
}

// DaysUntilExpiry calculates the number of days until the certificate expires
// Returns -1 if the certificate has no expiry date set
func (c *SSLCertificate) DaysUntilExpiry() int {
	if c.ExpiresAt == nil {
		return -1
	}
	duration := time.Until(*c.ExpiresAt)
	return int(duration.Hours() / 24)
}

// IsExpired checks if the certificate has expired
func (c *SSLCertificate) IsExpired() bool {
	if c.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*c.ExpiresAt)
}

// SSLCertificateRepository defines the interface for SSL certificate data access
// Requirements: 2.4, 3.1, 3.6
type SSLCertificateRepository interface {
	// Create creates a new SSL certificate record
	Create(ctx context.Context, cert *SSLCertificate) error

	// GetByID retrieves a certificate by its ID
	GetByID(ctx context.Context, id uuid.UUID) (*SSLCertificate, error)

	// GetByDomainID retrieves a certificate by domain ID
	GetByDomainID(ctx context.Context, domainID uuid.UUID) (*SSLCertificate, error)

	// GetByDomainName retrieves a certificate by domain name (case-insensitive)
	GetByDomainName(ctx context.Context, domainName string) (*SSLCertificate, error)

	// Update updates an existing certificate
	Update(ctx context.Context, cert *SSLCertificate) error

	// Delete deletes a certificate by ID
	Delete(ctx context.Context, id uuid.UUID) error

	// ListExpiringCertificates returns certificates expiring within the specified days
	// Requirements: 3.1 - Check certificate expiration daily
	ListExpiringCertificates(ctx context.Context, withinDays int) ([]*SSLCertificate, error)

	// UpdateStatus updates only the status of a certificate
	// Requirements: 1.7, 1.8 - Update domain.ssl_status during provisioning
	UpdateStatus(ctx context.Context, id uuid.UUID, status CertificateStatus) error

	// IncrementFailures increments the renewal failure counter and updates last attempt time
	// Requirements: 3.3 - Retry renewal every 24 hours if failed
	IncrementFailures(ctx context.Context, id uuid.UUID) error

	// ResetFailures resets the renewal failure counter (called after successful renewal)
	ResetFailures(ctx context.Context, id uuid.UUID) error

	// ListByStatus returns all certificates with the specified status
	ListByStatus(ctx context.Context, status CertificateStatus) ([]*SSLCertificate, error)

	// CountActive returns the count of active certificates
	// Requirements: 9.1 - Support at least 10,000 active certificates
	CountActive(ctx context.Context) (int, error)
}


// PostgresSSLCertificateRepository implements SSLCertificateRepository using PostgreSQL
type PostgresSSLCertificateRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresSSLCertificateRepository creates a new PostgresSSLCertificateRepository instance
func NewPostgresSSLCertificateRepository(pool *pgxpool.Pool) *PostgresSSLCertificateRepository {
	return &PostgresSSLCertificateRepository{pool: pool}
}

// Create inserts a new SSL certificate into the database
// Requirements: 2.4 - Store certificate metadata in database
func (r *PostgresSSLCertificateRepository) Create(ctx context.Context, cert *SSLCertificate) error {
	if !cert.Status.IsValid() {
		return ErrInvalidStatus
	}

	query := `
		INSERT INTO ssl_certificates (
			id, domain_id, domain_name, status, issuer, serial_number,
			issued_at, expires_at, last_renewal_attempt, renewal_failures,
			storage_path, created_at, updated_at
		)
		VALUES ($1, $2, LOWER($3), $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING created_at, updated_at
	`

	now := time.Now().UTC()
	if cert.ID == uuid.Nil {
		cert.ID = uuid.New()
	}

	err := r.pool.QueryRow(ctx, query,
		cert.ID,
		cert.DomainID,
		cert.DomainName,
		cert.Status,
		cert.Issuer,
		cert.SerialNumber,
		cert.IssuedAt,
		cert.ExpiresAt,
		cert.LastRenewalAttempt,
		cert.RenewalFailures,
		cert.StoragePath,
		now,
		now,
	).Scan(&cert.CreatedAt, &cert.UpdatedAt)

	if err != nil {
		// Check for unique constraint violation (certificate already exists for domain)
		if strings.Contains(err.Error(), "idx_ssl_certificates_domain") {
			return ErrCertificateExists
		}
		return fmt.Errorf("failed to create ssl certificate: %w", err)
	}

	return nil
}

// GetByID retrieves a certificate by its ID
func (r *PostgresSSLCertificateRepository) GetByID(ctx context.Context, id uuid.UUID) (*SSLCertificate, error) {
	query := `
		SELECT 
			id, domain_id, domain_name, status, issuer, serial_number,
			issued_at, expires_at, last_renewal_attempt, renewal_failures,
			storage_path, created_at, updated_at
		FROM ssl_certificates
		WHERE id = $1
	`

	cert := &SSLCertificate{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&cert.ID,
		&cert.DomainID,
		&cert.DomainName,
		&cert.Status,
		&cert.Issuer,
		&cert.SerialNumber,
		&cert.IssuedAt,
		&cert.ExpiresAt,
		&cert.LastRenewalAttempt,
		&cert.RenewalFailures,
		&cert.StoragePath,
		&cert.CreatedAt,
		&cert.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCertificateNotFound
		}
		return nil, fmt.Errorf("failed to get ssl certificate by ID: %w", err)
	}

	return cert, nil
}


// GetByDomainID retrieves a certificate by domain ID
func (r *PostgresSSLCertificateRepository) GetByDomainID(ctx context.Context, domainID uuid.UUID) (*SSLCertificate, error) {
	query := `
		SELECT 
			id, domain_id, domain_name, status, issuer, serial_number,
			issued_at, expires_at, last_renewal_attempt, renewal_failures,
			storage_path, created_at, updated_at
		FROM ssl_certificates
		WHERE domain_id = $1
	`

	cert := &SSLCertificate{}
	err := r.pool.QueryRow(ctx, query, domainID).Scan(
		&cert.ID,
		&cert.DomainID,
		&cert.DomainName,
		&cert.Status,
		&cert.Issuer,
		&cert.SerialNumber,
		&cert.IssuedAt,
		&cert.ExpiresAt,
		&cert.LastRenewalAttempt,
		&cert.RenewalFailures,
		&cert.StoragePath,
		&cert.CreatedAt,
		&cert.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCertificateNotFound
		}
		return nil, fmt.Errorf("failed to get ssl certificate by domain ID: %w", err)
	}

	return cert, nil
}

// GetByDomainName retrieves a certificate by domain name (case-insensitive)
// Requirements: 9.2 - Efficient certificate lookup O(1) by domain
func (r *PostgresSSLCertificateRepository) GetByDomainName(ctx context.Context, domainName string) (*SSLCertificate, error) {
	query := `
		SELECT 
			id, domain_id, domain_name, status, issuer, serial_number,
			issued_at, expires_at, last_renewal_attempt, renewal_failures,
			storage_path, created_at, updated_at
		FROM ssl_certificates
		WHERE LOWER(domain_name) = LOWER($1)
	`

	cert := &SSLCertificate{}
	err := r.pool.QueryRow(ctx, query, domainName).Scan(
		&cert.ID,
		&cert.DomainID,
		&cert.DomainName,
		&cert.Status,
		&cert.Issuer,
		&cert.SerialNumber,
		&cert.IssuedAt,
		&cert.ExpiresAt,
		&cert.LastRenewalAttempt,
		&cert.RenewalFailures,
		&cert.StoragePath,
		&cert.CreatedAt,
		&cert.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCertificateNotFound
		}
		return nil, fmt.Errorf("failed to get ssl certificate by domain name: %w", err)
	}

	return cert, nil
}

// Update updates an existing certificate
func (r *PostgresSSLCertificateRepository) Update(ctx context.Context, cert *SSLCertificate) error {
	if !cert.Status.IsValid() {
		return ErrInvalidStatus
	}

	query := `
		UPDATE ssl_certificates
		SET 
			domain_name = LOWER($1),
			status = $2,
			issuer = $3,
			serial_number = $4,
			issued_at = $5,
			expires_at = $6,
			last_renewal_attempt = $7,
			renewal_failures = $8,
			storage_path = $9,
			updated_at = $10
		WHERE id = $11
	`

	now := time.Now().UTC()
	result, err := r.pool.Exec(ctx, query,
		cert.DomainName,
		cert.Status,
		cert.Issuer,
		cert.SerialNumber,
		cert.IssuedAt,
		cert.ExpiresAt,
		cert.LastRenewalAttempt,
		cert.RenewalFailures,
		cert.StoragePath,
		now,
		cert.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update ssl certificate: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrCertificateNotFound
	}

	cert.UpdatedAt = now
	return nil
}


// Delete deletes a certificate by ID
func (r *PostgresSSLCertificateRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM ssl_certificates WHERE id = $1`

	result, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete ssl certificate: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrCertificateNotFound
	}

	return nil
}

// ListExpiringCertificates returns certificates expiring within the specified days
// Requirements: 3.1 - Check certificate expiration daily
// Requirements: 3.6 - Update ssl_expires_at in database
func (r *PostgresSSLCertificateRepository) ListExpiringCertificates(ctx context.Context, withinDays int) ([]*SSLCertificate, error) {
	query := `
		SELECT 
			id, domain_id, domain_name, status, issuer, serial_number,
			issued_at, expires_at, last_renewal_attempt, renewal_failures,
			storage_path, created_at, updated_at
		FROM ssl_certificates
		WHERE status = 'active'
		  AND expires_at IS NOT NULL
		  AND expires_at <= NOW() + ($1 || ' days')::INTERVAL
		ORDER BY expires_at ASC
	`

	rows, err := r.pool.Query(ctx, query, withinDays)
	if err != nil {
		return nil, fmt.Errorf("failed to list expiring certificates: %w", err)
	}
	defer rows.Close()

	var certs []*SSLCertificate
	for rows.Next() {
		cert := &SSLCertificate{}
		err := rows.Scan(
			&cert.ID,
			&cert.DomainID,
			&cert.DomainName,
			&cert.Status,
			&cert.Issuer,
			&cert.SerialNumber,
			&cert.IssuedAt,
			&cert.ExpiresAt,
			&cert.LastRenewalAttempt,
			&cert.RenewalFailures,
			&cert.StoragePath,
			&cert.CreatedAt,
			&cert.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan ssl certificate: %w", err)
		}
		certs = append(certs, cert)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating ssl certificates: %w", err)
	}

	return certs, nil
}

// UpdateStatus updates only the status of a certificate
// Requirements: 1.7, 1.8 - Update domain.ssl_status during provisioning
func (r *PostgresSSLCertificateRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status CertificateStatus) error {
	if !status.IsValid() {
		return ErrInvalidStatus
	}

	query := `
		UPDATE ssl_certificates
		SET status = $1, updated_at = $2
		WHERE id = $3
	`

	now := time.Now().UTC()
	result, err := r.pool.Exec(ctx, query, status, now, id)
	if err != nil {
		return fmt.Errorf("failed to update ssl certificate status: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrCertificateNotFound
	}

	return nil
}


// IncrementFailures increments the renewal failure counter and updates last attempt time
// Requirements: 3.3 - Retry renewal every 24 hours if failed
func (r *PostgresSSLCertificateRepository) IncrementFailures(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE ssl_certificates
		SET 
			renewal_failures = renewal_failures + 1,
			last_renewal_attempt = $1,
			updated_at = $1
		WHERE id = $2
	`

	now := time.Now().UTC()
	result, err := r.pool.Exec(ctx, query, now, id)
	if err != nil {
		return fmt.Errorf("failed to increment ssl certificate failures: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrCertificateNotFound
	}

	return nil
}

// ResetFailures resets the renewal failure counter (called after successful renewal)
func (r *PostgresSSLCertificateRepository) ResetFailures(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE ssl_certificates
		SET 
			renewal_failures = 0,
			last_renewal_attempt = $1,
			updated_at = $1
		WHERE id = $2
	`

	now := time.Now().UTC()
	result, err := r.pool.Exec(ctx, query, now, id)
	if err != nil {
		return fmt.Errorf("failed to reset ssl certificate failures: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrCertificateNotFound
	}

	return nil
}

// ListByStatus returns all certificates with the specified status
func (r *PostgresSSLCertificateRepository) ListByStatus(ctx context.Context, status CertificateStatus) ([]*SSLCertificate, error) {
	if !status.IsValid() {
		return nil, ErrInvalidStatus
	}

	query := `
		SELECT 
			id, domain_id, domain_name, status, issuer, serial_number,
			issued_at, expires_at, last_renewal_attempt, renewal_failures,
			storage_path, created_at, updated_at
		FROM ssl_certificates
		WHERE status = $1
		ORDER BY created_at DESC
	`

	rows, err := r.pool.Query(ctx, query, status)
	if err != nil {
		return nil, fmt.Errorf("failed to list certificates by status: %w", err)
	}
	defer rows.Close()

	var certs []*SSLCertificate
	for rows.Next() {
		cert := &SSLCertificate{}
		err := rows.Scan(
			&cert.ID,
			&cert.DomainID,
			&cert.DomainName,
			&cert.Status,
			&cert.Issuer,
			&cert.SerialNumber,
			&cert.IssuedAt,
			&cert.ExpiresAt,
			&cert.LastRenewalAttempt,
			&cert.RenewalFailures,
			&cert.StoragePath,
			&cert.CreatedAt,
			&cert.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan ssl certificate: %w", err)
		}
		certs = append(certs, cert)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating ssl certificates: %w", err)
	}

	return certs, nil
}

// CountActive returns the count of active certificates
// Requirements: 9.1 - Support at least 10,000 active certificates
func (r *PostgresSSLCertificateRepository) CountActive(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM ssl_certificates WHERE status = 'active'`

	var count int
	err := r.pool.QueryRow(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count active certificates: %w", err)
	}

	return count, nil
}

// Ensure PostgresSSLCertificateRepository implements SSLCertificateRepository interface
var _ SSLCertificateRepository = (*PostgresSSLCertificateRepository)(nil)
