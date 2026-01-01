#!/bin/bash
# ==============================================================================
# init-server.sh
# Server initialization script for Persistent Temp Mail
# Run this once on a fresh server to setup the environment
# ==============================================================================

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
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

info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

# Check if running as root
check_root() {
    if [ "$EUID" -ne 0 ]; then
        error "Please run as root (use sudo)"
    fi
}

# Update system packages
update_system() {
    log "Updating system packages..."

    if command -v apt-get &> /dev/null; then
        apt-get update
        apt-get upgrade -y
    elif command -v yum &> /dev/null; then
        yum update -y
    else
        error "Unsupported package manager"
    fi

    log "System updated"
}

# Install Docker
install_docker() {
    if command -v docker &> /dev/null; then
        log "Docker is already installed"
        docker --version
        return
    fi

    log "Installing Docker..."

    # Install prerequisites
    if command -v apt-get &> /dev/null; then
        apt-get install -y \
            ca-certificates \
            curl \
            gnupg \
            lsb-release

        # Add Docker's official GPG key
        mkdir -p /etc/apt/keyrings
        curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg

        # Set up Docker repository
        echo \
          "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
          $(lsb_release -cs) stable" | tee /etc/apt/sources.list.d/docker.list > /dev/null

        # Install Docker Engine
        apt-get update
        apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

    elif command -v yum &> /dev/null; then
        yum install -y yum-utils
        yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
        yum install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
        systemctl start docker
    fi

    # Enable Docker service
    systemctl enable docker
    systemctl start docker

    log "Docker installed successfully"
    docker --version
}

# Install Docker Compose (if not already installed)
install_docker_compose() {
    if command -v docker compose &> /dev/null; then
        log "Docker Compose is already installed"
        docker compose version
        return
    fi

    log "Docker Compose should be installed as a Docker plugin"
}

# Configure firewall
configure_firewall() {
    log "Configuring firewall..."

    if command -v ufw &> /dev/null; then
        # UFW (Ubuntu/Debian)
        ufw --force enable
        ufw default deny incoming
        ufw default allow outgoing
        ufw allow 22/tcp comment 'SSH'
        ufw allow 80/tcp comment 'HTTP'
        ufw allow 443/tcp comment 'HTTPS'
        ufw allow 25/tcp comment 'SMTP'
        ufw allow 587/tcp comment 'SMTP Submission'
        ufw allow 465/tcp comment 'SMTPS'
        ufw reload
        log "UFW firewall configured"

    elif command -v firewall-cmd &> /dev/null; then
        # firewalld (CentOS/RHEL)
        systemctl enable firewalld
        systemctl start firewalld
        firewall-cmd --permanent --add-service=http
        firewall-cmd --permanent --add-service=https
        firewall-cmd --permanent --add-service=smtp
        firewall-cmd --permanent --add-service=smtp-submission
        firewall-cmd --permanent --add-service=smtps
        firewall-cmd --permanent --add-service=ssh
        firewall-cmd --reload
        log "firewalld configured"

    else
        warn "No supported firewall found. Please configure manually."
    fi
}

# Create directory structure
create_directories() {
    log "Creating directory structure..."

    local base_dir="/opt/persistent-temp-mail"

    mkdir -p "${base_dir}"/{scripts,backups,nginx/conf.d,certbot/{conf,www},monitoring}
    mkdir -p "${base_dir}"/monitoring/{prometheus,grafana,loki,promtail,alertmanager}

    log "Directory structure created at ${base_dir}"
}

# Install required tools
install_tools() {
    log "Installing required tools..."

    if command -v apt-get &> /dev/null; then
        apt-get install -y \
            curl \
            wget \
            git \
            vim \
            htop \
            net-tools \
            postgresql-client \
            redis-tools \
            certbot \
            python3-certbot-nginx \
            openssl \
            awscli

    elif command -v yum &> /dev/null; then
        yum install -y \
            curl \
            wget \
            git \
            vim \
            htop \
            net-tools \
            postgresql \
            redis \
            certbot \
            python3-certbot-nginx \
            openssl \
            awscli
    fi

    log "Tools installed"
}

# Configure system limits
configure_limits() {
    log "Configuring system limits..."

    cat >> /etc/security/limits.conf <<EOF

# Persistent Temp Mail - System Limits
* soft nofile 65536
* hard nofile 65536
* soft nproc 65536
* hard nproc 65536
EOF

    # Sysctl optimizations
    cat >> /etc/sysctl.conf <<EOF

# Persistent Temp Mail - Network Optimizations
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 8192
net.ipv4.ip_local_port_range = 1024 65535
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_fin_timeout = 30
vm.max_map_count = 262144
EOF

    sysctl -p

    log "System limits configured"
}

# Setup log rotation
setup_log_rotation() {
    log "Setting up log rotation..."

    cat > /etc/logrotate.d/ptm <<EOF
/var/log/ptm-*.log {
    daily
    missingok
    rotate 14
    compress
    delaycompress
    notifempty
    create 0644 root root
    sharedscripts
    postrotate
        systemctl reload docker > /dev/null 2>&1 || true
    endscript
}
EOF

    log "Log rotation configured"
}

# Create deployment user
create_deploy_user() {
    log "Creating deployment user..."

    if id "ptm-deploy" &>/dev/null; then
        log "User ptm-deploy already exists"
    else
        useradd -m -s /bin/bash ptm-deploy
        usermod -aG docker ptm-deploy
        log "User ptm-deploy created"
    fi

    # Setup SSH directory
    mkdir -p /home/ptm-deploy/.ssh
    chmod 700 /home/ptm-deploy/.ssh
    chown -R ptm-deploy:ptm-deploy /home/ptm-deploy/.ssh

    info "Add your SSH public key to: /home/ptm-deploy/.ssh/authorized_keys"
}

# Setup swap (if not exists)
setup_swap() {
    if [ -f /swapfile ]; then
        log "Swap already configured"
        return
    fi

    log "Creating 2GB swap file..."

    fallocate -l 2G /swapfile
    chmod 600 /swapfile
    mkswap /swapfile
    swapon /swapfile

    # Make swap permanent
    echo '/swapfile none swap sw 0 0' >> /etc/fstab

    log "Swap configured"
}

# Display summary
display_summary() {
    echo ""
    echo "=============================================================================="
    echo -e "${GREEN}Server Initialization Complete!${NC}"
    echo "=============================================================================="
    echo ""
    echo "Next Steps:"
    echo ""
    echo "1. Clone the repository:"
    echo "   cd /opt/persistent-temp-mail"
    echo "   git clone https://github.com/welldanyogia/persistent-temp-mail.git ."
    echo ""
    echo "2. Configure environment variables:"
    echo "   cp .env.example .env.production"
    echo "   vim .env.production"
    echo ""
    echo "3. Setup SSL certificates:"
    echo "   certbot certonly --nginx -d webrana.id -d www.webrana.id"
    echo "   certbot certonly --nginx -d api.webrana.id"
    echo "   certbot certonly --nginx -d mail.webrana.id"
    echo ""
    echo "4. Deploy the application:"
    echo "   ./scripts/deploy.sh deploy"
    echo ""
    echo "5. Setup automated backups:"
    echo "   cp scripts/cron/backup-cron /etc/cron.d/ptm-backup"
    echo ""
    echo "=============================================================================="
    echo ""
}

# Main execution
main() {
    log "Starting server initialization for Persistent Temp Mail..."
    echo ""

    check_root
    update_system
    install_docker
    install_docker_compose
    install_tools
    configure_firewall
    create_directories
    configure_limits
    setup_log_rotation
    create_deploy_user
    setup_swap

    display_summary
}

main "$@"
