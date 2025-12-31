-- Rollback migration 001_create_users_table

BEGIN;

DROP TRIGGER IF EXISTS users_updated_at ON users;
DROP TABLE IF EXISTS users CASCADE;
DROP FUNCTION IF EXISTS update_updated_at_column() CASCADE;

COMMIT;
