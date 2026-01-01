#!/bin/bash
# ==============================================================================
# deploy.sh
# Production deployment script
# ==============================================================================

set -euo pipefail

# Configuration
DEPLOY_DIR="/opt/persistent-temp-mail"
COMPOSE_FILE="docker-compose.prod.yml"
VERSION="${VERSION:-latest}"

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

# Pre-deployment checks
pre_deploy_checks() {
    log "Running pre-deployment checks..."

    # Check Docker
    if ! command -v docker &> /dev/null; then
        error "Docker is not installed"
    fi

    # Check Docker Compose
    if ! command -v docker compose &> /dev/null; then
        error "Docker Compose is not installed"
    fi

    # Check environment file
    if [ ! -f "${DEPLOY_DIR}/.env.production" ]; then
        error "Production environment file not found: ${DEPLOY_DIR}/.env.production"
    fi

    # Validate required secrets
    source "${DEPLOY_DIR}/.env.production"

    if [ -z "${JWT_ACCESS_SECRET:-}" ]; then
        error "JWT_ACCESS_SECRET is not set"
    fi

    if [ -z "${DB_PASSWORD:-}" ]; then
        error "DB_PASSWORD is not set"
    fi

    log "Pre-deployment checks passed"
}

# Pull latest images
pull_images() {
    log "Pulling latest images..."

    docker compose -f "${DEPLOY_DIR}/${COMPOSE_FILE}" pull

    log "Images pulled successfully"
}

# Run database migrations
run_migrations() {
    log "Running database migrations..."

    docker compose -f "${DEPLOY_DIR}/${COMPOSE_FILE}" run --rm \
        backend /app/migrate up

    log "Migrations completed"
}

# Deploy services
deploy_services() {
    log "Deploying services..."

    # Stop existing containers gracefully
    docker compose -f "${DEPLOY_DIR}/${COMPOSE_FILE}" down --timeout 30

    # Start new containers
    docker compose -f "${DEPLOY_DIR}/${COMPOSE_FILE}" up -d

    log "Services deployed"
}

# Health check
health_check() {
    log "Running health checks..."

    local max_attempts=30
    local attempt=1

    while [ $attempt -le $max_attempts ]; do
        if curl -sf http://localhost:8080/health > /dev/null; then
            log "Backend is healthy"
            break
        fi

        log "Waiting for backend... (attempt ${attempt}/${max_attempts})"
        sleep 2
        ((attempt++))
    done

    if [ $attempt -gt $max_attempts ]; then
        error "Health check failed after ${max_attempts} attempts"
    fi

    # Check frontend
    attempt=1
    while [ $attempt -le $max_attempts ]; do
        if curl -sf http://localhost:3000 > /dev/null; then
            log "Frontend is healthy"
            break
        fi

        log "Waiting for frontend... (attempt ${attempt}/${max_attempts})"
        sleep 2
        ((attempt++))
    done

    log "All health checks passed"
}

# Rollback
rollback() {
    log "Rolling back to previous version..."

    docker compose -f "${DEPLOY_DIR}/${COMPOSE_FILE}" down

    # Use previous image tag
    VERSION="${PREVIOUS_VERSION:-}" docker compose -f "${DEPLOY_DIR}/${COMPOSE_FILE}" up -d

    log "Rollback completed"
}

# Cleanup
cleanup() {
    log "Cleaning up unused resources..."

    docker system prune -f
    docker image prune -a -f --filter "until=168h"

    log "Cleanup completed"
}

# Main
main() {
    cd "${DEPLOY_DIR}"

    case "${1:-deploy}" in
        deploy)
            pre_deploy_checks
            pull_images
            run_migrations
            deploy_services
            health_check
            cleanup
            log "Deployment completed successfully!"
            ;;
        rollback)
            rollback
            health_check
            ;;
        health)
            health_check
            ;;
        *)
            echo "Usage: $0 {deploy|rollback|health}"
            exit 1
            ;;
    esac
}

main "$@"
