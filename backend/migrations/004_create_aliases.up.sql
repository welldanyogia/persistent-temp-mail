-- Migration: 004_create_aliases
-- Description: Create aliases table for email alias management
-- Requirements: 1.9, 6.1-6.5, 7.3 (Email Alias Management)

BEGIN;

-- Create aliases table
CREATE TABLE aliases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    domain_id UUID NOT NULL,
    local_part VARCHAR(64) NOT NULL,
    full_address VARCHAR(320) NOT NULL,
    description TEXT,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'utc'),
    updated_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'utc'),

    -- Foreign Keys
    CONSTRAINT fk_aliases_user FOREIGN KEY (user_id)
        REFERENCES users (id)
        ON DELETE CASCADE,
    CONSTRAINT fk_aliases_domain FOREIGN KEY (domain_id)
        REFERENCES domains (id)
        ON DELETE CASCADE,

    -- Constraints
    -- RFC 5321: local part max 64 chars, pattern ^[a-z0-9._%+-]+$
    -- No leading/trailing dots, no consecutive dots
    CONSTRAINT aliases_local_part_valid CHECK (
        local_part ~* '^[a-z0-9._%+-]+$' AND
        char_length(local_part) BETWEEN 1 AND 64 AND
        local_part NOT LIKE '.%' AND
        local_part NOT LIKE '%.' AND
        local_part NOT LIKE '%..%'
    ),
    -- RFC 5321: full email address max 320 chars
    CONSTRAINT aliases_full_address_valid CHECK (
        full_address ~* '^[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}$' AND
        char_length(full_address) <= 320
    ),
    -- Description max 500 chars
    CONSTRAINT aliases_description_length CHECK (
        description IS NULL OR char_length(description) <= 500
    )
);

-- Indexes
-- Global uniqueness of full_address (case-insensitive)
CREATE UNIQUE INDEX idx_aliases_full_address ON aliases (LOWER(full_address));

-- User's aliases lookup (for listing user's aliases with pagination)
CREATE INDEX idx_aliases_user_id ON aliases (user_id, created_at DESC);

-- Domain's aliases lookup (for filtering by domain)
CREATE INDEX idx_aliases_domain_id ON aliases (domain_id, created_at DESC);

-- Active aliases lookup (for filtering active aliases)
CREATE INDEX idx_aliases_active ON aliases (is_active, user_id) WHERE is_active = true;

-- Trigger for updated_at (reuse function from 001_create_users_table)
CREATE TRIGGER aliases_updated_at
    BEFORE UPDATE ON aliases
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Comments
COMMENT ON TABLE aliases IS 'Email aliases for receiving mail under verified domains';
COMMENT ON COLUMN aliases.user_id IS 'Owner of the alias';
COMMENT ON COLUMN aliases.domain_id IS 'Domain this alias belongs to';
COMMENT ON COLUMN aliases.local_part IS 'Part before @ symbol (RFC 5321 compliant, max 64 chars)';
COMMENT ON COLUMN aliases.full_address IS 'Complete email address (local_part@domain_name, lowercase)';
COMMENT ON COLUMN aliases.description IS 'User note about alias purpose (max 500 chars)';
COMMENT ON COLUMN aliases.is_active IS 'Inactive aliases reject incoming mail';

COMMIT;
