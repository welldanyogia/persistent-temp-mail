-- Migration: 002_create_sessions_table
-- Description: Create sessions table for refresh token management and brute force protection
-- Requirements: 3.6 (SHA-256 token hash storage), 2.3 (brute force protection)

BEGIN;

-- Create sessions table
CREATE TABLE sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    token_hash VARCHAR(128) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'utc'),
    ip_address INET,
    user_agent TEXT,

    -- Foreign Keys
    CONSTRAINT fk_sessions_user FOREIGN KEY (user_id)
        REFERENCES users (id)
        ON DELETE CASCADE,

    -- Constraints
    CONSTRAINT sessions_expires_future CHECK (expires_at > created_at)
);

-- Indexes
CREATE UNIQUE INDEX idx_sessions_token_hash ON sessions (token_hash);
CREATE INDEX idx_sessions_user_id ON sessions (user_id, expires_at DESC);
-- Note: Partial index removed because NOW() is not IMMUTABLE
-- Expired sessions should be cleaned up by a scheduled job instead
CREATE INDEX idx_sessions_expires_at ON sessions (expires_at);

-- Comments
COMMENT ON TABLE sessions IS 'User authentication sessions and refresh tokens';
COMMENT ON COLUMN sessions.token_hash IS 'SHA-256 hash of the refresh token';
COMMENT ON COLUMN sessions.expires_at IS 'Token expiration timestamp (UTC)';

-- Create failed_login_attempts table for brute force protection
CREATE TABLE failed_login_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) NOT NULL,
    ip_address INET NOT NULL,
    attempted_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'utc'),
    
    -- Constraints
    CONSTRAINT failed_login_email_valid CHECK (
        email ~* '^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$'
    )
);

-- Indexes for failed_login_attempts
CREATE INDEX idx_failed_login_email ON failed_login_attempts (LOWER(email), attempted_at DESC);
CREATE INDEX idx_failed_login_ip ON failed_login_attempts (ip_address, attempted_at DESC);
CREATE INDEX idx_failed_login_cleanup ON failed_login_attempts (attempted_at);

-- Comments
COMMENT ON TABLE failed_login_attempts IS 'Track failed login attempts for brute force protection';
COMMENT ON COLUMN failed_login_attempts.email IS 'Email address used in failed login attempt';
COMMENT ON COLUMN failed_login_attempts.ip_address IS 'IP address of the failed attempt';
COMMENT ON COLUMN failed_login_attempts.attempted_at IS 'Timestamp of the failed attempt';

COMMIT;
