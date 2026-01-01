#!/bin/bash
# ==============================================================================
# ssl-renew.sh
# SSL Certificate renewal script for Let's Encrypt
# Handles both initial certificate issuance and renewal
# ==============================================================================

set -euo pipefail

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "${SCRIPT_DIR}")"
CERTBOT_DIR="${PROJECT_DIR}/certbot"
WEBROOT_DIR="${CERTBOT_DIR}/www"
LETSENCRYPT_DIR="${CERTBOT_DIR}/conf"
LOG_FILE="/var/log/ssl-renewal.log"

# Domains to manage
DOMAINS=(
    "webrana.id"
    "www.webrana.id"
    "api.webrana.id"
    "mail.webrana.id"
)

# Email for Let's Encrypt notifications
EMAIL="${SSL_EMAIL:-admin@webrana.id}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() {
    local msg="[$(date +'%Y-%m-%d %H:%M:%S')] $1"
    echo -e "${GREEN}${msg}${NC}"
    echo "${msg}" >> "${LOG_FILE}" 2>/dev/null || true
}

error() {
    local msg="[$(date +'%Y-%m-%d %H:%M:%S')] ERROR: $1"
    echo -e "${RED}${msg}${NC}" >&2
    echo "${msg}" >> "${LOG_FILE}" 2>/dev/null || true
    exit 1
}

warn() {
    local msg="[$(date +'%Y-%m-%d %H:%M:%S')] WARNING: $1"
    echo -e "${YELLOW}${msg}${NC}"
    echo "${msg}" >> "${LOG_FILE}" 2>/dev/null || true
}

# Ensure directories exist
setup_directories() {
    log "Setting up directories..."
    mkdir -p "${WEBROOT_DIR}/.well-known/acme-challenge"
    mkdir -p "${LETSENCRYPT_DIR}"
    mkdir -p "${CERTBOT_DIR}/renewal-hooks/deploy"
    mkdir -p "${CERTBOT_DIR}/renewal-hooks/pre"
    mkdir -p "${CERTBOT_DIR}/renewal-hooks/post"
    
    # Set permissions
    chmod 755 "${WEBROOT_DIR}"
    chmod 700 "${LETSENCRYPT_DIR}"
}

# Check if running in Docker
is_docker() {
    command -v docker &> /dev/null && docker ps &> /dev/null
}

# Run certbot command
run_certbot() {
    local cmd="$1"
    
    if is_docker; then
        log "Running certbot via Docker..."
        docker run --rm \
            -v "${LETSENCRYPT_DIR}:/etc/letsencrypt" \
            -v "${WEBROOT_DIR}:/var/www/certbot" \
            certbot/certbot:latest \
            ${cmd}
    else
        log "Running certbot directly..."
        certbot ${cmd}
    fi
}

# Issue new certificate
issue_certificate() {
    local domain="$1"
    log "Issuing certificate for: ${domain}"
    
    local domain_args=""
    if [ "${domain}" == "webrana.id" ]; then
        domain_args="-d webrana.id -d www.webrana.id"
    else
        domain_args="-d ${domain}"
    fi
    
    run_certbot "certonly \
        --webroot \
        --webroot-path=/var/www/certbot \
        --email ${EMAIL} \
        --agree-tos \
        --no-eff-email \
        --rsa-key-size 4096 \
        --non-interactive \
        ${domain_args}"
}

# Renew all certificates
renew_certificates() {
    log "Renewing certificates..."
    
    run_certbot "renew \
        --webroot \
        --webroot-path=/var/www/certbot \
        --non-interactive \
        --deploy-hook '/etc/letsencrypt/renewal-hooks/deploy/reload-nginx.sh'"
    
    log "Renewal process completed"
}

# Check certificate status
check_certificates() {
    log "Checking certificate status..."
    
    run_certbot "certificates"
}

# Force renewal (for testing or emergency)
force_renew() {
    local domain="$1"
    log "Force renewing certificate for: ${domain}"
    
    run_certbot "certonly \
        --webroot \
        --webroot-path=/var/www/certbot \
        --email ${EMAIL} \
        --agree-tos \
        --no-eff-email \
        --rsa-key-size 4096 \
        --non-interactive \
        --force-renewal \
        -d ${domain}"
}

# Reload Nginx after renewal
reload_nginx() {
    log "Reloading Nginx..."
    
    if is_docker; then
        if docker ps --format '{{.Names}}' | grep -q "^ptm-nginx$"; then
            docker exec ptm-nginx nginx -t && \
            docker exec ptm-nginx nginx -s reload
            log "Nginx reloaded successfully"
        else
            warn "Nginx container not found"
        fi
    else
        if systemctl is-active --quiet nginx; then
            nginx -t && systemctl reload nginx
            log "Nginx reloaded successfully"
        else
            warn "Nginx service not running"
        fi
    fi
}

# Dry run for testing
dry_run() {
    log "Running dry-run renewal..."
    
    run_certbot "renew --dry-run"
}

# Initial setup - issue certificates for all domains
initial_setup() {
    log "Starting initial SSL setup..."
    
    setup_directories
    
    # Issue certificate for main domain (includes www)
    issue_certificate "webrana.id"
    
    # Issue certificate for API subdomain
    issue_certificate "api.webrana.id"
    
    # Issue certificate for mail subdomain
    issue_certificate "mail.webrana.id"
    
    reload_nginx
    
    log "Initial SSL setup completed"
}

# Show certificate expiry dates
show_expiry() {
    log "Certificate expiry dates:"
    
    for domain in "${DOMAINS[@]}"; do
        cert_file="${LETSENCRYPT_DIR}/live/${domain}/fullchain.pem"
        if [ -f "${cert_file}" ]; then
            expiry=$(openssl x509 -enddate -noout -in "${cert_file}" 2>/dev/null | cut -d= -f2)
            days_left=$(( ($(date -d "${expiry}" +%s) - $(date +%s)) / 86400 ))
            
            if [ ${days_left} -lt 7 ]; then
                echo -e "${RED}  ${domain}: ${expiry} (${days_left} days left - CRITICAL)${NC}"
            elif [ ${days_left} -lt 30 ]; then
                echo -e "${YELLOW}  ${domain}: ${expiry} (${days_left} days left - WARNING)${NC}"
            else
                echo -e "${GREEN}  ${domain}: ${expiry} (${days_left} days left)${NC}"
            fi
        else
            echo -e "${RED}  ${domain}: Certificate not found${NC}"
        fi
    done
}

# Usage
usage() {
    cat << EOF
Usage: $0 <command>

Commands:
    setup       Initial setup - issue certificates for all domains
    renew       Renew all certificates (if needed)
    force       Force renewal for a specific domain
    check       Check certificate status
    expiry      Show certificate expiry dates
    dry-run     Test renewal without making changes
    reload      Reload Nginx configuration

Examples:
    $0 setup                    # Initial certificate setup
    $0 renew                    # Renew certificates
    $0 force api.webrana.id     # Force renew specific domain
    $0 expiry                   # Check expiry dates
    $0 dry-run                  # Test renewal process

Environment Variables:
    SSL_EMAIL   Email for Let's Encrypt notifications (default: admin@webrana.id)
EOF
}

# Main
main() {
    case "${1:-}" in
        setup)
            initial_setup
            ;;
        renew)
            setup_directories
            renew_certificates
            reload_nginx
            ;;
        force)
            if [ -z "${2:-}" ]; then
                error "Domain required for force renewal"
            fi
            setup_directories
            force_renew "$2"
            reload_nginx
            ;;
        check)
            check_certificates
            ;;
        expiry)
            show_expiry
            ;;
        dry-run)
            dry_run
            ;;
        reload)
            reload_nginx
            ;;
        *)
            usage
            exit 1
            ;;
    esac
}

main "$@"
