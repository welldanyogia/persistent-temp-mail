#!/bin/bash
# ==============================================================================
# notify-renewal.sh
# Certbot post-renewal hook - Send notification after renewal attempt
# Location: /etc/letsencrypt/renewal-hooks/post/notify-renewal.sh
# ==============================================================================

set -euo pipefail

LOG_FILE="/var/log/certbot-renewal.log"

log() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] POST-HOOK: $1" | tee -a "${LOG_FILE}"
}

log "Certificate renewal process completed"

# Log renewed domains if available
if [ -n "${RENEWED_DOMAINS:-}" ]; then
    log "Renewed domains: ${RENEWED_DOMAINS}"
fi

# Check certificate expiry dates
CERT_DIR="/etc/letsencrypt/live"
if [ -d "${CERT_DIR}" ]; then
    log "Checking certificate expiry dates:"
    for domain_dir in "${CERT_DIR}"/*; do
        if [ -d "${domain_dir}" ] && [ -f "${domain_dir}/fullchain.pem" ]; then
            domain=$(basename "${domain_dir}")
            expiry=$(openssl x509 -enddate -noout -in "${domain_dir}/fullchain.pem" 2>/dev/null | cut -d= -f2)
            log "  ${domain}: expires ${expiry}"
        fi
    done
fi

# Optional: Send notification (uncomment and configure as needed)
# 
# # Slack notification
# if [ -n "${SLACK_WEBHOOK_URL:-}" ]; then
#     curl -s -X POST -H 'Content-type: application/json' \
#         --data "{\"text\":\"SSL Certificate renewed for: ${RENEWED_DOMAINS:-unknown}\"}" \
#         "${SLACK_WEBHOOK_URL}"
# fi
#
# # Email notification
# if [ -n "${ADMIN_EMAIL:-}" ]; then
#     echo "SSL Certificate renewed for: ${RENEWED_DOMAINS:-unknown}" | \
#         mail -s "SSL Certificate Renewal - $(hostname)" "${ADMIN_EMAIL}"
# fi

log "Post-renewal hook completed"
exit 0
