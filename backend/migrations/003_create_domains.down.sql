-- Rollback migration 003_create_domains

BEGIN;

DROP TRIGGER IF EXISTS domains_updated_at ON domains;
DROP TABLE IF EXISTS domains CASCADE;

COMMIT;
