#!/bin/bash
# ==============================================================================
# reload-nginx.sh
# Certbot deploy hook - Gracefully reload Nginx after certificate renewal
# Location: /etc/letsencrypt/renewal-hooks/deploy/reload-nginx.sh
# ==============================================================================

set -euo pipefail

# Configuration
LOG_FILE="/var/log/certbot-renewal.log"
NGINX_CONTAINER="ptm-nginx"

# Logging function
log() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] $1" | tee -a "${LOG_FILE}"
}

log "Certificate renewal detected for: ${RENEWED_DOMAINS:-unknown}"
log "Certificate lineage: ${RENEWED_LINEAGE:-unknown}"

# Check if running in Docker environment
if command -v docker &> /dev/null; then
    # Docker environment - reload Nginx container
    if docker ps --format '{{.Names}}' | grep -q "^${NGINX_CONTAINER}$"; then
        log "Reloading Nginx container: ${NGINX_CONTAINER}"
        
        # Test Nginx configuration first
        if docker exec "${NGINX_CONTAINER}" nginx -t 2>&1; then
            # Graceful reload - sends SIGHUP to master process
            docker exec "${NGINX_CONTAINER}" nginx -s reload
            log "Nginx reloaded successfully"
        else
            log "ERROR: Nginx configuration test failed, skipping reload"
            exit 1
        fi
    else
        log "WARNING: Nginx container '${NGINX_CONTAINER}' not found"
        
        # Try systemd reload as fallback
        if systemctl is-active --quiet nginx; then
            log "Falling back to systemd nginx reload"
            systemctl reload nginx
            log "Nginx reloaded via systemd"
        fi
    fi
else
    # Non-Docker environment - use systemctl or nginx directly
    if systemctl is-active --quiet nginx; then
        log "Reloading Nginx via systemd"
        
        # Test configuration first
        if nginx -t 2>&1; then
            systemctl reload nginx
            log "Nginx reloaded successfully"
        else
            log "ERROR: Nginx configuration test failed"
            exit 1
        fi
    elif command -v nginx &> /dev/null; then
        log "Reloading Nginx directly"
        
        if nginx -t 2>&1; then
            nginx -s reload
            log "Nginx reloaded successfully"
        else
            log "ERROR: Nginx configuration test failed"
            exit 1
        fi
    else
        log "ERROR: No Nginx found to reload"
        exit 1
    fi
fi

log "Deploy hook completed successfully"
exit 0
