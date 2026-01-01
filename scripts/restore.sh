#!/bin/bash
# ==============================================================================
# restore.sh
# Disaster recovery script for Persistent Temp Mail
# ==============================================================================

set -euo pipefail

# Configuration
BACKUP_DIR="/backups"
DB_HOST="${DB_HOST:-postgres}"
DB_PORT="${DB_PORT:-5432}"
DB_USER="${DB_USER:-postgres}"
DB_NAME="${DB_NAME:-persistent_temp_mail}"
DB_PASSWORD="${DB_PASSWORD}"
ENCRYPTION_KEY="${BACKUP_ENCRYPTION_KEY:-}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() {
    echo -e "${GREEN}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $1"
}

error() {
    echo -e "${RED}[$(date +'%Y-%m-%d %H:%M:%S')] ERROR:${NC} $1" >&2
    exit 1
}

warn() {
    echo -e "${YELLOW}[$(date +'%Y-%m-%d %H:%M:%S')] WARNING:${NC} $1"
}

# Decrypt backup if encrypted
decrypt_backup() {
    local file="$1"

    if [[ "${file}" == *.enc ]]; then
        if [ -z "${ENCRYPTION_KEY}" ]; then
            error "Encrypted backup but no encryption key provided"
        fi

        log "Decrypting backup..."
        local decrypted="${file%.enc}"

        openssl enc -aes-256-cbc -d -pbkdf2 \
            -in "${file}" \
            -out "${decrypted}" \
            -pass pass:"${ENCRYPTION_KEY}"

        echo "${decrypted}"
    else
        echo "${file}"
    fi
}

# Restore PostgreSQL
restore_postgres() {
    local backup_file="$1"

    log "Restoring PostgreSQL from: ${backup_file}"

    # Decrypt if needed
    backup_file=$(decrypt_backup "${backup_file}")

    # Drop and recreate database
    warn "This will DROP the existing database. Continue? (y/N)"
    read -r confirm
    if [[ "${confirm}" != "y" && "${confirm}" != "Y" ]]; then
        log "Restore cancelled"
        exit 0
    fi

    log "Dropping existing database..."
    PGPASSWORD="${DB_PASSWORD}" psql \
        -h "${DB_HOST}" \
        -p "${DB_PORT}" \
        -U "${DB_USER}" \
        -c "DROP DATABASE IF EXISTS ${DB_NAME};"

    log "Creating new database..."
    PGPASSWORD="${DB_PASSWORD}" psql \
        -h "${DB_HOST}" \
        -p "${DB_PORT}" \
        -U "${DB_USER}" \
        -c "CREATE DATABASE ${DB_NAME};"

    log "Restoring data..."
    PGPASSWORD="${DB_PASSWORD}" pg_restore \
        -h "${DB_HOST}" \
        -p "${DB_PORT}" \
        -U "${DB_USER}" \
        -d "${DB_NAME}" \
        --no-owner \
        --no-privileges \
        "${backup_file}"

    log "PostgreSQL restore completed"
}

# Restore Redis
restore_redis() {
    local backup_file="$1"

    log "Restoring Redis from: ${backup_file}"

    backup_file=$(decrypt_backup "${backup_file}")

    # Stop Redis, replace dump, restart
    docker stop ptm-redis

    gunzip -c "${backup_file}" > /tmp/dump.rdb
    docker cp /tmp/dump.rdb ptm-redis:/data/dump.rdb

    docker start ptm-redis

    log "Redis restore completed"
}

# List available backups
list_backups() {
    log "Available backups in ${BACKUP_DIR}:"
    ls -la "${BACKUP_DIR}"/ptm_backup_* 2>/dev/null || echo "No backups found"
}

# Main
main() {
    case "${1:-}" in
        postgres)
            restore_postgres "${2:-}"
            ;;
        redis)
            restore_redis "${2:-}"
            ;;
        list)
            list_backups
            ;;
        *)
            echo "Usage: $0 {postgres|redis|list} [backup_file]"
            exit 1
            ;;
    esac
}

main "$@"
