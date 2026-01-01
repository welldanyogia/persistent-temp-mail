-- Rollback migration 008_create_ssl_certificates
-- Requirements: 2.4 - Drop ssl_certificates table

BEGIN;

DROP TRIGGER IF EXISTS ssl_certificates_updated_at ON ssl_certificates;
DROP TABLE IF EXISTS ssl_certificates CASCADE;

COMMIT;
