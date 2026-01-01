-- Create attachments table for storing attachment metadata
-- Requirements: 5.1-5.10

CREATE TABLE IF NOT EXISTS attachments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email_id UUID NOT NULL REFERENCES emails(id) ON DELETE CASCADE,
    filename VARCHAR(255) NOT NULL,
    content_type VARCHAR(255) NOT NULL DEFAULT 'application/octet-stream',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    storage_key VARCHAR(512) NOT NULL,
    storage_url VARCHAR(1024) NOT NULL,
    checksum VARCHAR(64) NOT NULL,
    created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT (NOW() AT TIME ZONE 'utc')
);

-- Index for fetching attachments by email
CREATE INDEX idx_attachments_email_id ON attachments (email_id);

-- Index for storage key lookups
CREATE UNIQUE INDEX idx_attachments_storage_key ON attachments (storage_key);

-- Comment on table
COMMENT ON TABLE attachments IS 'Stores attachment metadata for emails';
