-- Migration: 003_create_domains
-- Description: Create domains table for custom domain management
-- Requirements: FR-DOM-001 to FR-DOM-006 (Domain Management)

BEGIN;

-- Create domains table
CREATE TABLE domains (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    domain_name VARCHAR(253) NOT NULL,
    verification_token VARCHAR(64) NOT NULL,
    is_verified BOOLEAN NOT NULL DEFAULT false,
    verified_at TIMESTAMP,
    ssl_enabled BOOLEAN NOT NULL DEFAULT false,
    ssl_expires_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'utc'),
    updated_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'utc'),

    -- Foreign Keys
    CONSTRAINT fk_domains_user FOREIGN KEY (user_id)
        REFERENCES users (id)
        ON DELETE CASCADE,

    -- Constraints
    -- RFC 1035: domain name max 253 chars, labels max 63 chars
    CONSTRAINT domains_name_format CHECK (
        domain_name ~* '^([a-z0-9]+(-[a-z0-9]+)*\.)+[a-z]{2,}$' AND
        char_length(domain_name) <= 253
    ),
    CONSTRAINT domains_verification_token_format CHECK (
        verification_token ~ '^vrf_[a-f0-9]{64}$'
    ),
    CONSTRAINT domains_verified_at_check CHECK (
        (is_verified = false AND verified_at IS NULL) OR
        (is_verified = true AND verified_at IS NOT NULL)
    ),
    CONSTRAINT domains_ssl_expires_check CHECK (
        (ssl_enabled = false AND ssl_expires_at IS NULL) OR
        (ssl_enabled = true AND ssl_expires_at IS NOT NULL)
    )
);

-- Indexes
-- Unique domain name (globally unique across all users)
CREATE UNIQUE INDEX idx_domains_name ON domains (LOWER(domain_name));

-- User's domains lookup (for listing user's domains)
CREATE INDEX idx_domains_user_id ON domains (user_id, created_at DESC);

-- Verified domains lookup (for filtering by status)
CREATE INDEX idx_domains_verified ON domains (is_verified, created_at DESC);

-- SSL expiry monitoring (for auto-renewal jobs)
CREATE INDEX idx_domains_ssl_expires ON domains (ssl_expires_at)
    WHERE ssl_enabled = true AND ssl_expires_at IS NOT NULL;

-- Trigger for updated_at (reuse function from 001_create_users_table)
CREATE TRIGGER domains_updated_at
    BEFORE UPDATE ON domains
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Comments
COMMENT ON TABLE domains IS 'Custom domains for receiving email';
COMMENT ON COLUMN domains.user_id IS 'Owner of the domain';
COMMENT ON COLUMN domains.domain_name IS 'Domain name (RFC 1035 compliant, max 253 chars)';
COMMENT ON COLUMN domains.verification_token IS 'DNS TXT record verification token (vrf_ prefix + 64 hex chars)';
COMMENT ON COLUMN domains.is_verified IS 'Whether DNS verification has passed';
COMMENT ON COLUMN domains.verified_at IS 'Timestamp when domain was verified';
COMMENT ON COLUMN domains.ssl_enabled IS 'Whether SSL certificate is active';
COMMENT ON COLUMN domains.ssl_expires_at IS 'SSL certificate expiration date';

COMMIT;
