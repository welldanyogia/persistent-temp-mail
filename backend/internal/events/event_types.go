package events

import "time"

// Event type constants
const (
	EventTypeConnected       = "connected"
	EventTypeHeartbeat       = "heartbeat"
	EventTypeNewEmail        = "new_email"
	EventTypeEmailDeleted    = "email_deleted"
	EventTypeAliasCreated    = "alias_created"
	EventTypeAliasDeleted    = "alias_deleted"
	EventTypeDomainVerified  = "domain_verified"
	EventTypeDomainDeleted   = "domain_deleted"
	EventTypeConnectionLimit = "connection_limit"
	EventTypeError           = "error"
)

// ConnectedEvent is sent when a client establishes an SSE connection.
type ConnectedEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
}

// HeartbeatEvent is sent periodically to keep the connection alive.
type HeartbeatEvent struct {
	Timestamp time.Time `json:"timestamp"`
}

// NewEmailEvent is sent when a new email is received.
type NewEmailEvent struct {
	ID             string    `json:"id"`
	AliasID        string    `json:"alias_id"`
	AliasEmail     string    `json:"alias_email"`
	FromAddress    string    `json:"from_address"`
	FromName       *string   `json:"from_name,omitempty"`
	Subject        *string   `json:"subject,omitempty"`
	PreviewText    string    `json:"preview_text"`
	ReceivedAt     time.Time `json:"received_at"`
	HasAttachments bool      `json:"has_attachments"`
	SizeBytes      int64     `json:"size_bytes"`
}

// EmailDeletedEvent is sent when an email is deleted.
type EmailDeletedEvent struct {
	ID        string    `json:"id"`
	AliasID   string    `json:"alias_id"`
	DeletedAt time.Time `json:"deleted_at"`
}

// AliasCreatedEvent is sent when a new alias is created.
type AliasCreatedEvent struct {
	ID           string    `json:"id"`
	EmailAddress string    `json:"email_address"`
	DomainID     string    `json:"domain_id"`
	CreatedAt    time.Time `json:"created_at"`
}

// AliasDeletedEvent is sent when an alias is deleted.
type AliasDeletedEvent struct {
	ID            string    `json:"id"`
	EmailAddress  string    `json:"email_address"`
	DeletedAt     time.Time `json:"deleted_at"`
	EmailsDeleted int       `json:"emails_deleted"`
}

// DomainVerifiedEvent is sent when a domain is verified.
type DomainVerifiedEvent struct {
	ID         string    `json:"id"`
	DomainName string    `json:"domain_name"`
	VerifiedAt time.Time `json:"verified_at"`
	SSLStatus  string    `json:"ssl_status"`
}

// DomainDeletedEvent is sent when a domain is deleted.
type DomainDeletedEvent struct {
	ID             string    `json:"id"`
	DomainName     string    `json:"domain_name"`
	DeletedAt      time.Time `json:"deleted_at"`
	AliasesDeleted int       `json:"aliases_deleted"`
	EmailsDeleted  int       `json:"emails_deleted"`
}

// ConnectionLimitEvent is sent when a user exceeds the connection limit.
type ConnectionLimitEvent struct {
	Message        string `json:"message"`
	MaxConnections int    `json:"max_connections"`
}

// ErrorEvent is sent when an error occurs.
type ErrorEvent struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
