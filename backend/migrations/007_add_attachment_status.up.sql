-- Add status column to attachments table for tracking upload failures
-- Requirements: 1.10 - Mark attachment as failed on permanent failure

-- Add status column with default 'active' for existing records
ALTER TABLE attachments 
ADD COLUMN IF NOT EXISTS status VARCHAR(20) NOT NULL DEFAULT 'active';

-- Add error_details column for storing failure information
ALTER TABLE attachments 
ADD COLUMN IF NOT EXISTS error_details TEXT;

-- Add retry_count column for tracking retry attempts
ALTER TABLE attachments 
ADD COLUMN IF NOT EXISTS retry_count INTEGER NOT NULL DEFAULT 0;

-- Add constraint to validate status values
ALTER TABLE attachments 
ADD CONSTRAINT attachments_status_valid 
CHECK (status IN ('active', 'failed', 'pending'));

-- Create index for filtering by status
CREATE INDEX IF NOT EXISTS idx_attachments_status ON attachments (status);

-- Create partial index for failed attachments (for monitoring/cleanup)
CREATE INDEX IF NOT EXISTS idx_attachments_failed ON attachments (created_at) 
WHERE status = 'failed';

-- Comment on columns
COMMENT ON COLUMN attachments.status IS 'Upload status: active (success), failed (permanent failure), pending (in progress)';
COMMENT ON COLUMN attachments.error_details IS 'Error details for failed uploads';
COMMENT ON COLUMN attachments.retry_count IS 'Number of retry attempts made';
