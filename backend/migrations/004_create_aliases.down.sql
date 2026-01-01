-- Rollback migration 004_create_aliases

BEGIN;

DROP TRIGGER IF EXISTS aliases_updated_at ON aliases;
DROP TABLE IF EXISTS aliases CASCADE;

COMMIT;
