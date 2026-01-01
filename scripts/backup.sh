#!/bin/bash
# ==============================================================================
# backup.sh
# Automated backup script for Persistent Temp Mail
# ==============================================================================

set -euo pipefail

# Configuration
BACKUP_DIR="/backups"
RETENTION_DAYS=30
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_NAME="ptm_backup_${TIMESTAMP}"

# Database configuration
DB_HOST="${DB_HOST:-postgres}"
DB_PORT="${DB_PORT:-5432}"
DB_USER="${DB_USER:-postgres}"
DB_NAME="${DB_NAME:-persistent_temp_mail}"
DB_PASSWORD="${DB_PASSWORD}"

# S3 configuration for offsite backup
S3_BUCKET="${S3_BACKUP_BUCKET:-}"
S3_ENDPOINT="${S3_ENDPOINT:-}"

# Encryption key (should be stored securely)
ENCRYPTION_KEY="${BACKUP_ENCRYPTION_KEY:-}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() {
    echo -e "${GREEN}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $1"
}

error() {
    echo -e "${RED}[$(date +'%Y-%m-%d %H:%M:%S')] ERROR:${NC} $1" >&2
}

warn() {
    echo -e "${YELLOW}[$(date +'%Y-%m-%d %H:%M:%S')] WARNING:${NC} $1"
}

# Create backup directory
mkdir -p "${BACKUP_DIR}"

# ==============================================================================
# PostgreSQL Backup
# ==============================================================================
backup_postgres() {
    log "Starting PostgreSQL backup..."

    local pg_backup_file="${BACKUP_DIR}/${BACKUP_NAME}_postgres.sql.gz"

    PGPASSWORD="${DB_PASSWORD}" pg_dump \
        -h "${DB_HOST}" \
        -p "${DB_PORT}" \
        -U "${DB_USER}" \
        -d "${DB_NAME}" \
        --format=custom \
        --compress=9 \
        --file="${pg_backup_file}"

    if [ $? -eq 0 ]; then
        log "PostgreSQL backup completed: ${pg_backup_file}"
        echo "${pg_backup_file}"
    else
        error "PostgreSQL backup failed"
        return 1
    fi
}

# ==============================================================================
# Redis Backup
# ==============================================================================
backup_redis() {
    log "Starting Redis backup..."

    local redis_backup_file="${BACKUP_DIR}/${BACKUP_NAME}_redis.rdb"

    # Trigger Redis BGSAVE
    redis-cli -h redis BGSAVE

    # Wait for background save to complete
    sleep 5

    # Copy the dump file
    docker cp ptm-redis:/data/dump.rdb "${redis_backup_file}"

    if [ $? -eq 0 ]; then
        log "Redis backup completed: ${redis_backup_file}"
        gzip "${redis_backup_file}"
        echo "${redis_backup_file}.gz"
    else
        warn "Redis backup failed (non-critical)"
    fi
}

# ==============================================================================
# SSL Certificates Backup
# ==============================================================================
backup_ssl() {
    log "Starting SSL certificates backup..."

    local ssl_backup_file="${BACKUP_DIR}/${BACKUP_NAME}_ssl.tar.gz"

    tar -czf "${ssl_backup_file}" \
        -C /etc/letsencrypt . \
        2>/dev/null || true

    if [ -f "${ssl_backup_file}" ]; then
        log "SSL backup completed: ${ssl_backup_file}"
        echo "${ssl_backup_file}"
    else
        warn "SSL backup failed (certificates may not exist)"
    fi
}

# ==============================================================================
# Encrypt Backup
# ==============================================================================
encrypt_backup() {
    local file="$1"

    if [ -z "${ENCRYPTION_KEY}" ]; then
        warn "Encryption key not set, skipping encryption"
        return
    fi

    log "Encrypting backup: ${file}"

    openssl enc -aes-256-cbc -salt -pbkdf2 \
        -in "${file}" \
        -out "${file}.enc" \
        -pass pass:"${ENCRYPTION_KEY}"

    rm "${file}"
    echo "${file}.enc"
}

# ==============================================================================
# Upload to S3
# ==============================================================================
upload_to_s3() {
    local file="$1"

    if [ -z "${S3_BUCKET}" ]; then
        warn "S3 bucket not configured, skipping upload"
        return
    fi

    log "Uploading to S3: ${file}"

    aws s3 cp "${file}" "s3://${S3_BUCKET}/$(basename ${file})" \
        --endpoint-url "${S3_ENDPOINT}"

    if [ $? -eq 0 ]; then
        log "Upload completed successfully"
    else
        error "S3 upload failed"
    fi
}

# ==============================================================================
# Cleanup Old Backups
# ==============================================================================
cleanup_old_backups() {
    log "Cleaning up backups older than ${RETENTION_DAYS} days..."

    find "${BACKUP_DIR}" -name "ptm_backup_*" -type f -mtime +${RETENTION_DAYS} -delete

    log "Cleanup completed"
}

# ==============================================================================
# Verify Backup
# ==============================================================================
verify_backup() {
    local file="$1"

    log "Verifying backup integrity: ${file}"

    if [ -f "${file}" ]; then
        local size=$(stat -f%z "${file}" 2>/dev/null || stat -c%s "${file}")
        if [ "${size}" -gt 0 ]; then
            log "Backup verified: ${size} bytes"
            return 0
        fi
    fi

    error "Backup verification failed"
    return 1
}

# ==============================================================================
# Main Execution
# ==============================================================================
main() {
    log "Starting backup process..."

    # Create backup files
    local pg_backup=$(backup_postgres)
    local redis_backup=$(backup_redis)
    local ssl_backup=$(backup_ssl)

    # Encrypt backups
    if [ -n "${ENCRYPTION_KEY}" ]; then
        pg_backup=$(encrypt_backup "${pg_backup}")
        [ -n "${redis_backup}" ] && redis_backup=$(encrypt_backup "${redis_backup}")
        [ -n "${ssl_backup}" ] && ssl_backup=$(encrypt_backup "${ssl_backup}")
    fi

    # Verify backups
    verify_backup "${pg_backup}"

    # Upload to S3
    upload_to_s3 "${pg_backup}"
    [ -n "${redis_backup}" ] && upload_to_s3 "${redis_backup}"
    [ -n "${ssl_backup}" ] && upload_to_s3 "${ssl_backup}"

    # Cleanup old backups
    cleanup_old_backups

    log "Backup process completed successfully!"
}

main "$@"
