-- Create emails table for storing received emails
-- Requirements: 3.1-3.5, 4.1-4.12

CREATE TABLE IF NOT EXISTS emails (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    alias_id UUID NOT NULL REFERENCES aliases(id) ON DELETE CASCADE,
    sender_address VARCHAR(320) NOT NULL,
    sender_name VARCHAR(255),
    subject VARCHAR(998),
    body_html TEXT,
    body_text TEXT,
    headers JSONB DEFAULT '{}',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    is_read BOOLEAN NOT NULL DEFAULT false,
    raw_email BYTEA,
    received_at TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT (NOW() AT TIME ZONE 'utc'),
    created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT (NOW() AT TIME ZONE 'utc')
);

-- Index for inbox queries (most common query)
CREATE INDEX idx_emails_alias_id_received ON emails (alias_id, received_at DESC);

-- Index for unread messages
CREATE INDEX idx_emails_is_read ON emails (alias_id, is_read, received_at DESC) WHERE is_read = false;

-- Full-text search index on subject
CREATE INDEX idx_emails_subject_gin ON emails USING gin (to_tsvector('english', COALESCE(subject, '')));

-- Comment on table
COMMENT ON TABLE emails IS 'Stores received emails for aliases';
