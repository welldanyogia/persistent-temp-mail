-- Rollback migration 002_create_sessions_table

BEGIN;

DROP TABLE IF EXISTS failed_login_attempts CASCADE;
DROP TABLE IF EXISTS sessions CASCADE;

COMMIT;
