-- Migration: 008_create_ssl_certificates
-- Description: Create ssl_certificates table for SSL/TLS certificate management
-- Requirements: 2.4 - Store certificate metadata in database

BEGIN;

-- Create ssl_certificates table
CREATE TABLE ssl_certificates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    domain_id UUID NOT NULL,
    domain_name VARCHAR(253) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    issuer VARCHAR(255),
    serial_number VARCHAR(128),
    issued_at TIMESTAMP,
    expires_at TIMESTAMP,
    last_renewal_attempt TIMESTAMP,
    renewal_failures INTEGER NOT NULL DEFAULT 0,
    storage_path VARCHAR(512),
    created_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'utc'),
    updated_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'utc'),

    -- Foreign Keys
    CONSTRAINT fk_ssl_certificates_domain FOREIGN KEY (domain_id)
        REFERENCES domains (id)
        ON DELETE CASCADE,

    -- Constraints
    -- Status validation for certificate lifecycle
    CONSTRAINT ssl_status_valid CHECK (status IN (
        'pending', 'provisioning', 'active', 'expired', 'revoked', 'failed'
    )),
    
    -- Domain name format validation (RFC 1035)
    CONSTRAINT ssl_domain_name_format CHECK (
        domain_name ~* '^([a-z0-9]+(-[a-z0-9]+)*\.)+[a-z]{2,}$' AND
        char_length(domain_name) <= 253
    ),
    
    -- Renewal failures must be non-negative
    CONSTRAINT ssl_renewal_failures_check CHECK (renewal_failures >= 0),
    
    -- Expiry date validation for active certificates
    CONSTRAINT ssl_expires_at_check CHECK (
        (status NOT IN ('active', 'expired') AND expires_at IS NULL) OR
        (status IN ('active', 'expired') AND expires_at IS NOT NULL)
    ),
    
    -- Issued date validation for active certificates
    CONSTRAINT ssl_issued_at_check CHECK (
        (status NOT IN ('active', 'expired', 'revoked') AND issued_at IS NULL) OR
        (status IN ('active', 'expired', 'revoked') AND issued_at IS NOT NULL)
    )
);

-- Indexes
-- Unique domain lookup (one certificate per domain)
CREATE UNIQUE INDEX idx_ssl_certificates_domain ON ssl_certificates (domain_id);

-- Domain name lookup for SMTP SNI (O(1) lookup requirement)
CREATE UNIQUE INDEX idx_ssl_certificates_domain_name ON ssl_certificates (LOWER(domain_name));

-- Expiry monitoring for renewal scheduler (Requirements: 3.1, 3.6)
CREATE INDEX idx_ssl_certificates_expires ON ssl_certificates (expires_at)
    WHERE status = 'active';

-- Status filtering for monitoring and management
CREATE INDEX idx_ssl_certificates_status ON ssl_certificates (status);

-- Failed certificates for retry logic
CREATE INDEX idx_ssl_certificates_failed ON ssl_certificates (last_renewal_attempt)
    WHERE status = 'failed' OR renewal_failures > 0;

-- Trigger for updated_at (reuse function from 001_create_users_table)
CREATE TRIGGER ssl_certificates_updated_at
    BEFORE UPDATE ON ssl_certificates
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Comments
COMMENT ON TABLE ssl_certificates IS 'SSL/TLS certificate metadata for custom domains';
COMMENT ON COLUMN ssl_certificates.domain_id IS 'Reference to the domain this certificate belongs to';
COMMENT ON COLUMN ssl_certificates.domain_name IS 'Domain name for certificate (cached for quick lookup)';
COMMENT ON COLUMN ssl_certificates.status IS 'Certificate status: pending, provisioning, active, expired, revoked, failed';
COMMENT ON COLUMN ssl_certificates.issuer IS 'Certificate issuer (e.g., Let''s Encrypt Authority X3)';
COMMENT ON COLUMN ssl_certificates.serial_number IS 'Certificate serial number from CA';
COMMENT ON COLUMN ssl_certificates.issued_at IS 'Certificate issuance timestamp';
COMMENT ON COLUMN ssl_certificates.expires_at IS 'Certificate expiration timestamp';
COMMENT ON COLUMN ssl_certificates.last_renewal_attempt IS 'Last renewal attempt timestamp';
COMMENT ON COLUMN ssl_certificates.renewal_failures IS 'Count of consecutive renewal failures';
COMMENT ON COLUMN ssl_certificates.storage_path IS 'Path to encrypted certificate files on disk';

COMMIT;
