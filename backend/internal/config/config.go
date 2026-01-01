package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration
type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	JWT      JWTConfig
	Domain   DomainConfig
	Storage  StorageConfig
	Alias    AliasConfig
	SMTP     SMTPConfig
	SSE      SSEConfig
	SSL      SSLConfig
	Redis    RedisConfig
	Logging  LoggingConfig
}

// LoggingConfig holds logging configuration
// Requirements: 14.3 - Application logs SHALL use structured JSON format
// Requirements: 14.4 - Application logs SHALL include correlation IDs for request tracing
// Requirements: 14.10 - System SHALL support log level configuration per environment
type LoggingConfig struct {
	Level     string // Log level: debug, info, warn, error (default: info)
	Format    string // Log format: json, text (default: json)
	Output    string // Log output: stdout, stderr, or file path (default: stdout)
	AddSource bool   // Add source file and line number to log entries (default: false)
}

// RedisConfig holds Redis connection configuration
// Requirements: 10.3 - Redis connectivity check in health endpoint
type RedisConfig struct {
	Host     string // Redis host (default: localhost)
	Port     string // Redis port (default: 6379)
	Password string // Redis password (optional)
	DB       int    // Redis database number (default: 0)
	Enabled  bool   // Whether Redis is enabled (default: false)
}

// SSLConfig holds SSL certificate management configuration
// Requirements: 1.2 - Use Let's Encrypt as the Certificate Authority
type SSLConfig struct {
	// Let's Encrypt configuration
	LetsEncryptEmail   string // Email for Let's Encrypt account registration
	LetsEncryptStaging bool   // Use staging environment for testing (default: false)

	// DNS provider configuration (Cloudflare)
	CloudflareAPIToken string // Cloudflare API token for DNS-01 challenge
	CloudflareZoneID   string // Cloudflare Zone ID (optional)

	// Storage configuration
	CertStoragePath   string // Base path for certificate storage (default: /var/lib/tempmail/certs)
	CertEncryptionKey string // 32-byte hex-encoded AES-256 encryption key

	// Renewal configuration
	CertRenewalDays       int           // Days before expiry to renew (default: 30)
	CertCheckInterval     time.Duration // How often to check for expiring certs (default: 24h)
	ProvisionTimeout      time.Duration // Timeout for provisioning operations (default: 5 minutes)
	MaxConcurrentProvision int          // Max concurrent provisioning operations (default: 10)

	// Feature flag
	Enabled bool // Whether SSL management is enabled (default: false)
}

// SSEConfig holds Server-Sent Events configuration
// Requirements: 1.5, 1.7, 2.1 - Connection limits, timeout, heartbeat
type SSEConfig struct {
	HeartbeatInterval     time.Duration // Heartbeat interval (default: 30 seconds)
	ConnectionTimeout     time.Duration // Connection timeout (default: 1 hour)
	MaxConnectionsPerUser int           // Max connections per user (default: 10)
	EventBufferSize       int           // Event buffer size for replay (default: 100)
}

// SMTPConfig holds SMTP server configuration
type SMTPConfig struct {
	Port                int           // SMTP port (default: 25)
	Hostname            string        // Server hostname for EHLO response
	MaxConnections      int           // Maximum concurrent connections (default: 100)
	MaxConnectionsPerIP int           // Maximum connections per IP (default: 5)
	ConnectionTimeout   time.Duration // Idle connection timeout (default: 5 minutes)
	MaxMessageSize      int64         // Maximum message size in bytes (default: 25 MB)
	MaxRecipients       int           // Maximum recipients per message (default: 100)
	RateLimitPerMinute  int           // Rate limit per IP per minute (default: 20)
	TLSCertFile         string        // Path to TLS certificate file
	TLSKeyFile          string        // Path to TLS private key file
	TLSEnabled          bool          // Whether STARTTLS is enabled
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Host string
	Port string
}

// DatabaseConfig holds PostgreSQL connection configuration
type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	SSLMode  string
}

// JWTConfig holds JWT token configuration
type JWTConfig struct {
	AccessSecret        string
	RefreshSecret       string
	AccessTokenExpiry   time.Duration
	RefreshTokenExpiry  time.Duration
	Issuer              string
}

// DomainConfig holds domain management configuration
type DomainConfig struct {
	MailServer  string // Expected MX record target (e.g., "mail.webrana.id")
	TXTPrefix   string // TXT record subdomain prefix
	DomainLimit int    // Max domains per user
	SSLEnabled  bool   // Whether SSL provisioning is enabled
}

// StorageConfig holds S3/MinIO configuration for attachment storage
type StorageConfig struct {
	Endpoint             string        // S3/MinIO endpoint (e.g., "localhost:9000" or "s3.amazonaws.com")
	Region               string        // AWS region (e.g., "us-east-1")
	AccessKeyID          string        // Access key ID
	SecretAccessKey      string        // Secret access key
	Bucket               string        // Bucket name for attachments
	UseSSL               bool          // Use HTTPS for S3 connection
	PresignedURLExpiry   time.Duration // Pre-signed URL expiration time (default: 15 minutes)
	LargeFileThreshold   int64         // File size threshold for returning pre-signed URL instead of streaming (default: 10 MB)
	OrphanCleanupEnabled bool          // Enable orphan cleanup job (default: true)
	OrphanCleanupInterval time.Duration // Interval between cleanup runs (default: 24 hours)
	OrphanCleanupAge     time.Duration // Age threshold for orphan files (default: 7 days)
}

// AliasConfig holds email alias management configuration
type AliasConfig struct {
	MaxAliasesPerUser int // Maximum number of aliases per user (default: 50)
}

// Load reads configuration from environment variables
func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Host: getEnv("SERVER_HOST", "0.0.0.0"),
			Port: getEnv("SERVER_PORT", "8080"),
		},
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     getEnv("DB_USER", "postgres"),
			Password: getEnv("DB_PASSWORD", ""),
			DBName:   getEnv("DB_NAME", "persistent_temp_mail"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		JWT: JWTConfig{
			AccessSecret:       getEnv("JWT_ACCESS_SECRET", ""),
			RefreshSecret:      getEnv("JWT_REFRESH_SECRET", ""),
			AccessTokenExpiry:  getDurationEnv("JWT_ACCESS_EXPIRY", 15*time.Minute),
			RefreshTokenExpiry: getDurationEnv("JWT_REFRESH_EXPIRY", 7*24*time.Hour),
			Issuer:             getEnv("JWT_ISSUER", "persistent-temp-mail"),
		},
		Domain: DomainConfig{
			MailServer:  getEnv("DOMAIN_MAIL_SERVER", "mail.webrana.id"),
			TXTPrefix:   getEnv("DOMAIN_TXT_PREFIX", "_tempmail-verification"),
			DomainLimit: getIntEnv("DOMAIN_LIMIT", 5),
			SSLEnabled:  getBoolEnv("DOMAIN_SSL_ENABLED", false),
		},
		Storage: StorageConfig{
			Endpoint:              getEnv("S3_ENDPOINT", "localhost:9000"),
			Region:                getEnv("S3_REGION", "us-east-1"),
			AccessKeyID:           getEnv("S3_ACCESS_KEY_ID", ""),
			SecretAccessKey:       getEnv("S3_SECRET_ACCESS_KEY", ""),
			Bucket:                getEnv("S3_BUCKET", "persistent-temp-mail-attachments"),
			UseSSL:                getBoolEnv("S3_USE_SSL", false),
			PresignedURLExpiry:    getDurationEnv("S3_PRESIGNED_URL_EXPIRY", 15*time.Minute),
			LargeFileThreshold:    getInt64Env("S3_LARGE_FILE_THRESHOLD", 10*1024*1024), // 10 MB
			OrphanCleanupEnabled:  getBoolEnv("S3_ORPHAN_CLEANUP_ENABLED", true),
			OrphanCleanupInterval: getDurationEnv("S3_ORPHAN_CLEANUP_INTERVAL", 24*time.Hour),
			OrphanCleanupAge:      getDurationEnv("S3_ORPHAN_CLEANUP_AGE", 7*24*time.Hour), // 7 days
		},
		Alias: AliasConfig{
			MaxAliasesPerUser: getIntEnv("ALIAS_MAX_PER_USER", 50),
		},
		SMTP: SMTPConfig{
			Port:                getIntEnv("SMTP_PORT", 25),
			Hostname:            getEnv("SMTP_HOSTNAME", "mail.webrana.id"),
			MaxConnections:      getIntEnv("SMTP_MAX_CONNECTIONS", 100),
			MaxConnectionsPerIP: getIntEnv("SMTP_MAX_CONNECTIONS_PER_IP", 5),
			ConnectionTimeout:   getDurationEnv("SMTP_CONNECTION_TIMEOUT", 5*time.Minute),
			MaxMessageSize:      getInt64Env("SMTP_MAX_MESSAGE_SIZE", 25*1024*1024), // 25 MB
			MaxRecipients:       getIntEnv("SMTP_MAX_RECIPIENTS", 100),
			RateLimitPerMinute:  getIntEnv("SMTP_RATE_LIMIT_PER_MINUTE", 20),
			TLSCertFile:         getEnv("SMTP_TLS_CERT_FILE", ""),
			TLSKeyFile:          getEnv("SMTP_TLS_KEY_FILE", ""),
			TLSEnabled:          getBoolEnv("SMTP_TLS_ENABLED", false),
		},
		SSE: SSEConfig{
			HeartbeatInterval:     getDurationEnv("SSE_HEARTBEAT_INTERVAL", 30*time.Second),
			ConnectionTimeout:     getDurationEnv("SSE_CONNECTION_TIMEOUT", 1*time.Hour),
			MaxConnectionsPerUser: getIntEnv("SSE_MAX_CONNECTIONS_PER_USER", 10),
			EventBufferSize:       getIntEnv("SSE_EVENT_BUFFER_SIZE", 100),
		},
		SSL: SSLConfig{
			// Let's Encrypt configuration
			LetsEncryptEmail:   getEnv("LETSENCRYPT_EMAIL", ""),
			LetsEncryptStaging: getBoolEnv("LETSENCRYPT_STAGING", false),

			// Cloudflare DNS provider configuration
			CloudflareAPIToken: getEnv("CLOUDFLARE_API_TOKEN", ""),
			CloudflareZoneID:   getEnv("CLOUDFLARE_ZONE_ID", ""),

			// Certificate storage configuration
			CertStoragePath:   getEnv("CERT_STORAGE_PATH", "/var/lib/tempmail/certs"),
			CertEncryptionKey: getEnv("CERT_ENCRYPTION_KEY", ""),

			// Renewal configuration
			CertRenewalDays:        getIntEnv("CERT_RENEWAL_DAYS", 30),
			CertCheckInterval:      getDurationEnv("CERT_CHECK_INTERVAL", 24*time.Hour),
			ProvisionTimeout:       getDurationEnv("CERT_PROVISION_TIMEOUT", 5*time.Minute),
			MaxConcurrentProvision: getIntEnv("CERT_MAX_CONCURRENT_PROVISION", 10),

			// Feature flag
			Enabled: getBoolEnv("SSL_ENABLED", false),
		},
		Redis: RedisConfig{
			Host:     getEnv("REDIS_HOST", "localhost"),
			Port:     getEnv("REDIS_PORT", "6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getIntEnv("REDIS_DB", 0),
			Enabled:  getBoolEnv("REDIS_ENABLED", false),
		},
		Logging: LoggingConfig{
			Level:     getEnv("LOG_LEVEL", "info"),
			Format:    getEnv("LOG_FORMAT", "json"),
			Output:    getEnv("LOG_OUTPUT", "stdout"),
			AddSource: getBoolEnv("LOG_ADD_SOURCE", false),
		},
	}
}

// DSN returns the PostgreSQL connection string
func (d *DatabaseConfig) DSN() string {
	return "host=" + d.Host +
		" port=" + d.Port +
		" user=" + d.User +
		" password=" + d.Password +
		" dbname=" + d.DBName +
		" sslmode=" + d.SSLMode
}

// getEnv returns environment variable value or default
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getDurationEnv returns duration from environment variable or default
func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if minutes, err := strconv.Atoi(value); err == nil {
			return time.Duration(minutes) * time.Minute
		}
	}
	return defaultValue
}

// getIntEnv returns int from environment variable or default
func getIntEnv(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

// getInt64Env returns int64 from environment variable or default
func getInt64Env(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
			return intVal
		}
	}
	return defaultValue
}

// getBoolEnv returns bool from environment variable or default
func getBoolEnv(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolVal, err := strconv.ParseBool(value); err == nil {
			return boolVal
		}
	}
	return defaultValue
}

// GetEncryptionKey returns the SSL certificate encryption key as bytes
// The key should be a 64-character hex string (32 bytes when decoded)
// Returns nil if the key is not configured or invalid
func (s *SSLConfig) GetEncryptionKey() []byte {
	if s.CertEncryptionKey == "" {
		return nil
	}

	// Decode hex string to bytes
	key := make([]byte, 32)
	n, err := decodeHex(s.CertEncryptionKey, key)
	if err != nil || n != 32 {
		return nil
	}

	return key
}

// decodeHex decodes a hex string into bytes
func decodeHex(s string, dst []byte) (int, error) {
	if len(s) != len(dst)*2 {
		return 0, fmt.Errorf("invalid hex string length")
	}

	for i := 0; i < len(dst); i++ {
		a, ok1 := fromHexChar(s[i*2])
		b, ok2 := fromHexChar(s[i*2+1])
		if !ok1 || !ok2 {
			return 0, fmt.Errorf("invalid hex character")
		}
		dst[i] = (a << 4) | b
	}

	return len(dst), nil
}

// fromHexChar converts a hex character to its value
func fromHexChar(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
