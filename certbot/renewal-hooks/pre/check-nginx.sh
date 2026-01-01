#!/bin/bash
# ==============================================================================
# check-nginx.sh
# Certbot pre-renewal hook - Verify Nginx is running before renewal
# Location: /etc/letsencrypt/renewal-hooks/pre/check-nginx.sh
# ==============================================================================

set -euo pipefail

LOG_FILE="/var/log/certbot-renewal.log"
NGINX_CONTAINER="ptm-nginx"

log() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] PRE-HOOK: $1" | tee -a "${LOG_FILE}"
}

log "Starting pre-renewal checks"

# Check if Nginx is running (required for webroot authentication)
if command -v docker &> /dev/null; then
    if docker ps --format '{{.Names}}' | grep -q "^${NGINX_CONTAINER}$"; then
        log "Nginx container is running"
    else
        log "ERROR: Nginx container is not running"
        log "Starting Nginx container..."
        docker start "${NGINX_CONTAINER}" || {
            log "ERROR: Failed to start Nginx container"
            exit 1
        }
        sleep 5
    fi
else
    if systemctl is-active --quiet nginx; then
        log "Nginx service is running"
    elif pgrep -x nginx > /dev/null; then
        log "Nginx process is running"
    else
        log "ERROR: Nginx is not running"
        exit 1
    fi
fi

# Verify webroot directory exists and is accessible
WEBROOT="/var/www/certbot"
if [ -d "${WEBROOT}" ]; then
    log "Webroot directory exists: ${WEBROOT}"
else
    log "Creating webroot directory: ${WEBROOT}"
    mkdir -p "${WEBROOT}"
fi

# Verify ACME challenge directory
ACME_DIR="${WEBROOT}/.well-known/acme-challenge"
if [ ! -d "${ACME_DIR}" ]; then
    log "Creating ACME challenge directory: ${ACME_DIR}"
    mkdir -p "${ACME_DIR}"
fi

log "Pre-renewal checks completed successfully"
exit 0
