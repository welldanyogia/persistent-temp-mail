-- Migration: 001_create_users_table
-- Description: Create users table for authentication
-- Requirements: 1.7 (bcrypt password storage)

BEGIN;

-- Enable required extensions
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Create utility function for updated_at trigger
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = (NOW() AT TIME ZONE 'utc');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create users table
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'utc'),
    updated_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'utc'),
    last_login_at TIMESTAMP,
    is_active BOOLEAN NOT NULL DEFAULT true,

    -- Constraints
    CONSTRAINT users_email_valid CHECK (
        email ~* '^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$'
    ),
    CONSTRAINT users_password_hash_length CHECK (char_length(password_hash) >= 60)
);

-- Indexes
CREATE UNIQUE INDEX idx_users_email ON users (LOWER(email));
CREATE INDEX idx_users_created_at ON users (created_at DESC);
CREATE INDEX idx_users_is_active ON users (is_active) WHERE is_active = true;

-- Trigger for updated_at
CREATE TRIGGER users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Comments
COMMENT ON TABLE users IS 'Authenticated user accounts';
COMMENT ON COLUMN users.email IS 'User login email (unique, case-insensitive)';
COMMENT ON COLUMN users.password_hash IS 'Bcrypt hash (cost factor 12+)';
COMMENT ON COLUMN users.is_active IS 'Soft disable flag for suspended accounts';

COMMIT;
