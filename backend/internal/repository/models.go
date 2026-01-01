package repository

import (
	"time"

	"github.com/google/uuid"
)

// User represents a user account in the database
type User struct {
	ID           uuid.UUID  `db:"id"`
	Email        string     `db:"email"`
	PasswordHash string     `db:"password_hash"`
	CreatedAt    time.Time  `db:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at"`
	LastLoginAt  *time.Time `db:"last_login_at"`
	IsActive     bool       `db:"is_active"`
}

// Session represents an authentication session in the database
type Session struct {
	ID        uuid.UUID  `db:"id"`
	UserID    uuid.UUID  `db:"user_id"`
	TokenHash string     `db:"token_hash"`
	ExpiresAt time.Time  `db:"expires_at"`
	CreatedAt time.Time  `db:"created_at"`
	IPAddress *string    `db:"ip_address"`
	UserAgent *string    `db:"user_agent"`
}

// FailedLoginAttempt represents a failed login attempt for brute force protection
type FailedLoginAttempt struct {
	ID          uuid.UUID `db:"id"`
	Email       string    `db:"email"`
	IPAddress   string    `db:"ip_address"`
	AttemptedAt time.Time `db:"attempted_at"`
}

// Alias represents an email alias in the database
type Alias struct {
	ID          uuid.UUID  `db:"id"`
	UserID      uuid.UUID  `db:"user_id"`
	DomainID    uuid.UUID  `db:"domain_id"`
	LocalPart   string     `db:"local_part"`
	FullAddress string     `db:"full_address"`
	Description *string    `db:"description"`
	IsActive    bool       `db:"is_active"`
	CreatedAt   time.Time  `db:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"`
}

// AliasWithStats represents an alias with computed statistics
type AliasWithStats struct {
	Alias
	DomainName          string     `db:"domain_name"`
	EmailCount          int        `db:"email_count"`
	LastEmailReceivedAt *time.Time `db:"last_email_received_at"`
	TotalSizeBytes      int64      `db:"total_size_bytes"`
}

// AliasStats represents detailed statistics for an alias
type AliasStats struct {
	EmailsToday     int         `json:"emails_today"`
	EmailsThisWeek  int         `json:"emails_this_week"`
	EmailsThisMonth int         `json:"emails_this_month"`
	TopSenders      []TopSender `json:"top_senders"`
}

// TopSender represents a frequent sender for an alias
type TopSender struct {
	Email string `json:"email"`
	Count int    `json:"count"`
}

// ListAliasParams holds parameters for listing aliases
type ListAliasParams struct {
	Page     int
	Limit    int
	DomainID *uuid.UUID
	Search   string
	Sort     string
	Order    string
}


// Email represents a received email in the database
type Email struct {
	ID            uuid.UUID         `db:"id"`
	AliasID       uuid.UUID         `db:"alias_id"`
	SenderAddress string            `db:"sender_address"`
	SenderName    *string           `db:"sender_name"`
	Subject       *string           `db:"subject"`
	BodyHTML      *string           `db:"body_html"`
	BodyText      *string           `db:"body_text"`
	Headers       map[string]string `db:"headers"`
	SizeBytes     int64             `db:"size_bytes"`
	IsRead        bool              `db:"is_read"`
	RawEmail      []byte            `db:"raw_email"`
	ReceivedAt    time.Time         `db:"received_at"`
	CreatedAt     time.Time         `db:"created_at"`
}

// Attachment represents an email attachment metadata in the database
type Attachment struct {
	ID           uuid.UUID `db:"id"`
	EmailID      uuid.UUID `db:"email_id"`
	Filename     string    `db:"filename"`
	ContentType  string    `db:"content_type"`
	SizeBytes    int64     `db:"size_bytes"`
	StorageKey   string    `db:"storage_key"`
	StorageURL   string    `db:"storage_url"`
	Checksum     string    `db:"checksum"`
	Status       string    `db:"status"`        // Requirements: 1.10 - Track attachment status
	ErrorDetails *string   `db:"error_details"` // Requirements: 1.10 - Store error details
	RetryCount   int       `db:"retry_count"`   // Requirements: 1.9 - Track retry attempts
	CreatedAt    time.Time `db:"created_at"`
}

// ListEmailParams holds parameters for listing emails
type ListEmailParams struct {
	Page           int
	Limit          int
	AliasID        *uuid.UUID
	Search         string
	FromDate       *time.Time
	ToDate         *time.Time
	HasAttachments *bool
	IsRead         *bool
	Sort           string
	Order          string
}

// EmailWithPreview represents an email with preview text for list responses
type EmailWithPreview struct {
	ID              uuid.UUID  `db:"id" json:"id"`
	AliasID         uuid.UUID  `db:"alias_id" json:"alias_id"`
	AliasEmail      string     `db:"alias_email" json:"alias_email"`
	FromAddress     string     `db:"from_address" json:"from_address"`
	FromName        *string    `db:"from_name" json:"from_name,omitempty"`
	Subject         *string    `db:"subject" json:"subject,omitempty"`
	PreviewText     string     `db:"preview_text" json:"preview_text"`
	ReceivedAt      time.Time  `db:"received_at" json:"received_at"`
	HasAttachments  bool       `db:"has_attachments" json:"has_attachments"`
	AttachmentCount int        `db:"attachment_count" json:"attachment_count"`
	SizeBytes       int64      `db:"size_bytes" json:"size_bytes"`
	IsRead          bool       `db:"is_read" json:"is_read"`
}

// InboxStats represents inbox statistics for a user
type InboxStats struct {
	TotalEmails     int               `json:"total_emails"`
	UnreadEmails    int               `json:"unread_emails"`
	TotalSizeBytes  int64             `json:"total_size_bytes"`
	EmailsToday     int               `json:"emails_today"`
	EmailsThisWeek  int               `json:"emails_this_week"`
	EmailsThisMonth int               `json:"emails_this_month"`
	EmailsPerAlias  []AliasEmailCount `json:"emails_per_alias"`
}

// AliasEmailCount represents email count per alias
type AliasEmailCount struct {
	AliasID    uuid.UUID `db:"alias_id" json:"alias_id"`
	AliasEmail string    `db:"alias_email" json:"alias_email"`
	Count      int       `db:"count" json:"count"`
}
