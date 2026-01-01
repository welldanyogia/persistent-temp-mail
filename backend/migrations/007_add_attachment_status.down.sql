-- Remove status tracking columns from attachments table

-- Drop indexes first
DROP INDEX IF EXISTS idx_attachments_failed;
DROP INDEX IF EXISTS idx_attachments_status;

-- Drop constraint
ALTER TABLE attachments DROP CONSTRAINT IF EXISTS attachments_status_valid;

-- Drop columns
ALTER TABLE attachments DROP COLUMN IF EXISTS retry_count;
ALTER TABLE attachments DROP COLUMN IF EXISTS error_details;
ALTER TABLE attachments DROP COLUMN IF EXISTS status;
