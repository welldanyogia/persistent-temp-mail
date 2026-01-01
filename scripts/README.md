# Deployment and Backup Scripts

This directory contains scripts for server initialization, deployment, backup, and disaster recovery for the Persistent Temp Mail application.

## Scripts Overview

### 1. `init-server.sh` - Server Initialization
**Purpose**: One-time server setup script for fresh installations.

**Features**:
- Docker and Docker Compose installation
- Firewall configuration (UFW/firewalld)
- System limits and network optimizations
- Directory structure creation
- Required tools installation (PostgreSQL client, Redis CLI, Certbot, AWS CLI)
- Deployment user creation
- Swap configuration
- Log rotation setup

**Usage**:
```bash
# Run as root
sudo ./init-server.sh
```

**Requirements**:
- Fresh Ubuntu 20.04+ or CentOS 7+ server
- Root access
- Minimum 2GB RAM, 2 CPU cores

---

### 2. `deploy.sh` - Production Deployment
**Purpose**: Automated deployment with health checks and rollback capability.

**Features**:
- Pre-deployment validation (Docker, environment variables, secrets)
- Docker image pulling
- Database migrations
- Graceful service deployment
- Health check verification
- Automatic cleanup of old images
- Rollback support

**Usage**:
```bash
# Deploy new version
./deploy.sh deploy

# Rollback to previous version
./deploy.sh rollback

# Check health status
./deploy.sh health
```

**Environment Variables**:
- `VERSION`: Docker image tag (default: latest)
- `PREVIOUS_VERSION`: Used for rollback
- Requires `.env.production` file in deploy directory

**Deployment Process**:
1. Pre-deployment checks (Docker, secrets validation)
2. Pull latest images from registry
3. Run database migrations
4. Graceful shutdown of existing containers (30s timeout)
5. Start new containers
6. Health check verification (30 attempts, 2s interval)
7. Cleanup unused resources

**Rollback Process**:
1. Stop current containers
2. Start containers with `PREVIOUS_VERSION` tag
3. Verify health checks

---

### 3. `backup.sh` - Automated Backup
**Purpose**: Complete backup of PostgreSQL, Redis, and SSL certificates.

**Features**:
- PostgreSQL full backup (pg_dump with compression)
- Redis RDB snapshot backup
- SSL certificates backup (Let's Encrypt)
- AES-256 encryption with PBKDF2
- S3 offsite upload (optional)
- Backup verification
- Automatic retention management (30 days default)

**Usage**:
```bash
# Manual backup
./backup.sh
```

**Environment Variables**:
```bash
# Database
DB_HOST=postgres
DB_PORT=5432
DB_USER=postgres
DB_NAME=persistent_temp_mail
DB_PASSWORD=<password>

# Backup storage
BACKUP_DIR=/backups
RETENTION_DAYS=30

# Encryption (recommended)
BACKUP_ENCRYPTION_KEY=<32-char-key>

# S3 offsite backup (optional)
S3_BACKUP_BUCKET=persistent-temp-mail-backups
S3_ENDPOINT=https://s3.amazonaws.com
```

**Backup Files Generated**:
- `ptm_backup_<timestamp>_postgres.sql.gz.enc` - Encrypted PostgreSQL backup
- `ptm_backup_<timestamp>_redis.rdb.gz.enc` - Encrypted Redis backup
- `ptm_backup_<timestamp>_ssl.tar.gz.enc` - Encrypted SSL certificates

**Security Features**:
- AES-256-CBC encryption with salt
- PBKDF2 key derivation
- Automatic cleanup of unencrypted files
- Backup integrity verification

---

### 4. `restore.sh` - Disaster Recovery
**Purpose**: Restore from encrypted backups.

**Features**:
- PostgreSQL database restore
- Redis data restore
- Automatic backup decryption
- Safety confirmations before destructive operations
- Backup listing

**Usage**:
```bash
# List available backups
./restore.sh list

# Restore PostgreSQL
./restore.sh postgres /backups/ptm_backup_20260101_020000_postgres.sql.gz.enc

# Restore Redis
./restore.sh redis /backups/ptm_backup_20260101_020000_redis.rdb.gz.enc
```

**WARNING**:
- PostgreSQL restore will DROP the existing database
- Requires manual confirmation
- Ensure services are stopped before restore

**Environment Variables**:
```bash
DB_HOST=postgres
DB_PORT=5432
DB_USER=postgres
DB_NAME=persistent_temp_mail
DB_PASSWORD=<password>
BACKUP_ENCRYPTION_KEY=<32-char-key>
```

---

### 5. `cron/backup-cron` - Automated Backup Schedule
**Purpose**: Crontab configuration for automated maintenance tasks.

**Features**:
- Daily full backup at 2:00 AM
- Hourly incremental backup (PostgreSQL WAL)
- Daily log cleanup (older than 7 days)
- Monthly backup verification

**Installation**:
```bash
sudo cp scripts/cron/backup-cron /etc/cron.d/ptm-backup
sudo chmod 644 /etc/cron.d/ptm-backup
sudo service cron reload
```

**Logs**:
- Backup logs: `/var/log/ptm-backup.log`

**Schedule**:
```
0 2 * * *   - Daily full backup (2 AM)
0 * * * *   - Hourly incremental backup
0 4 * * *   - Daily log cleanup (4 AM)
0 5 1 * *   - Monthly backup verification (1st, 5 AM)
```

---

### 6. `ssl-renew.sh` - SSL Certificate Management
**Purpose**: Manage Let's Encrypt SSL certificates for all domains.

**Features**:
- Initial certificate issuance for all domains
- Automatic renewal via certbot
- Certificate status checking
- Expiry date monitoring
- Nginx graceful reload after renewal
- Dry-run testing support

**Usage**:
```bash
# Initial setup - issue certificates for all domains
./ssl-renew.sh setup

# Renew certificates (if needed)
./ssl-renew.sh renew

# Force renewal for specific domain
./ssl-renew.sh force api.webrana.id

# Check certificate status
./ssl-renew.sh check

# Show expiry dates
./ssl-renew.sh expiry

# Test renewal (dry-run)
./ssl-renew.sh dry-run

# Reload Nginx
./ssl-renew.sh reload
```

**Environment Variables**:
```bash
SSL_EMAIL=admin@webrana.id  # Email for Let's Encrypt notifications
```

**Managed Domains**:
- webrana.id (+ www.webrana.id)
- api.webrana.id
- mail.webrana.id

**Certificate Locations**:
- Certificates: `certbot/conf/live/<domain>/fullchain.pem`
- Private keys: `certbot/conf/live/<domain>/privkey.pem`
- Chain: `certbot/conf/live/<domain>/chain.pem`

---

### 7. `cron/ssl-renewal-cron` - SSL Auto-Renewal Schedule
**Purpose**: Crontab configuration for automated SSL certificate renewal.

**Features**:
- Twice daily renewal check (recommended by Let's Encrypt)
- Weekly certificate expiry monitoring
- Monthly dry-run testing

**Installation**:
```bash
sudo cp scripts/cron/ssl-renewal-cron /etc/cron.d/ptm-ssl-renewal
sudo chmod 644 /etc/cron.d/ptm-ssl-renewal
sudo service cron reload
```

**Logs**:
- SSL renewal logs: `/var/log/ssl-renewal.log`

**Schedule**:
```
17 3,15 * * *  - Twice daily renewal check (3:17 AM/PM)
0 9 * * 0      - Weekly expiry check (Sunday 9 AM)
0 4 1 * *      - Monthly dry-run test (1st, 4 AM)
```

**Note**: Let's Encrypt certificates are valid for 90 days. Certbot automatically renews certificates when they have less than 30 days until expiry.

---

## Deployment Workflow

### Initial Server Setup

```bash
# 1. Run server initialization (as root)
sudo ./init-server.sh

# 2. Clone repository
cd /opt/persistent-temp-mail
git clone https://github.com/welldanyogia/persistent-temp-mail.git .

# 3. Configure environment
cp .env.example .env.production
vim .env.production  # Fill in production secrets

# 4. Setup SSL certificates
certbot certonly --nginx -d webrana.id -d www.webrana.id
certbot certonly --nginx -d api.webrana.id
certbot certonly --nginx -d mail.webrana.id

# 5. Setup automated backups
sudo cp scripts/cron/backup-cron /etc/cron.d/ptm-backup

# 6. Deploy application
./scripts/deploy.sh deploy
```

### Regular Deployment

```bash
# Pull latest code
git pull origin main

# Deploy new version
VERSION=v1.2.3 ./scripts/deploy.sh deploy

# If issues occur, rollback
PREVIOUS_VERSION=v1.2.2 ./scripts/deploy.sh rollback
```

### Disaster Recovery

```bash
# 1. List available backups
./scripts/restore.sh list

# 2. Stop services
docker compose -f docker-compose.prod.yml down

# 3. Restore database
./scripts/restore.sh postgres /backups/ptm_backup_20260101_020000_postgres.sql.gz.enc

# 4. Restore Redis (optional)
./scripts/restore.sh redis /backups/ptm_backup_20260101_020000_redis.rdb.gz.enc

# 5. Start services
./scripts/deploy.sh deploy
```

---

## Security Best Practices

### Secret Management

1. **Never commit secrets to version control**
   - Use `.env.production` (gitignored)
   - Store encryption keys in secure vault (e.g., AWS Secrets Manager, HashiCorp Vault)

2. **Encryption Keys**
   ```bash
   # Generate strong encryption key
   openssl rand -base64 32
   ```

3. **Database Passwords**
   ```bash
   # Generate strong password
   openssl rand -base64 24
   ```

### Backup Security

1. **Always encrypt backups** - Set `BACKUP_ENCRYPTION_KEY`
2. **Store encryption keys separately** from backups
3. **Use S3 bucket encryption** at rest
4. **Implement S3 bucket versioning** for backup protection
5. **Test restore procedures** monthly

### File Permissions

```bash
# Scripts should be executable by owner only
chmod 700 scripts/*.sh

# Cron files should be root-owned
sudo chown root:root /etc/cron.d/ptm-backup
sudo chmod 644 /etc/cron.d/ptm-backup

# Environment files should be restricted
chmod 600 .env.production
```

---

## Monitoring and Alerting

### Health Check Endpoints

- **Backend**: `http://localhost:8080/health`
- **Frontend**: `http://localhost:3000`

### Log Locations

- **Backup logs**: `/var/log/ptm-backup.log`
- **Docker logs**: `docker compose logs -f [service]`
- **Nginx logs**: `/var/log/nginx/access.log`, `/var/log/nginx/error.log`

### Common Issues

**Deployment fails health check**:
```bash
# Check container logs
docker compose -f docker-compose.prod.yml logs backend

# Check container status
docker compose -f docker-compose.prod.yml ps

# Manual health check
curl -v http://localhost:8080/health
```

**Backup fails**:
```bash
# Check backup logs
tail -f /var/log/ptm-backup.log

# Test database connection
PGPASSWORD=$DB_PASSWORD psql -h postgres -U postgres -d persistent_temp_mail -c "SELECT 1;"

# Test Redis connection
redis-cli -h redis ping
```

**Restore fails**:
```bash
# Verify backup file exists
ls -lh /backups/ptm_backup_*

# Test decryption
openssl enc -aes-256-cbc -d -pbkdf2 -in backup.enc -pass pass:"$BACKUP_ENCRYPTION_KEY"
```

---

## Maintenance Tasks

### Weekly

- Review backup logs
- Verify backup sizes are reasonable
- Check disk space usage

### Monthly

- Test restore procedure
- Review security updates
- Audit user access logs
- Verify SSL certificate renewal

### Quarterly

- Update Docker images
- Review and update firewall rules
- Disaster recovery drill
- Performance review

---

## Support and Troubleshooting

For issues or questions:
1. Check logs: `/var/log/ptm-*.log`
2. Review Docker logs: `docker compose logs`
3. Check system resources: `htop`, `df -h`
4. Review monitoring dashboards (Grafana)

**Emergency Contacts**:
- DevOps Lead: ATLAS (Team Beta)
- Security: SENTINEL
- Database: Contact database administrator

---

## Version History

- **v1.0.0** (2026-01-01): Initial release
  - Server initialization script
  - Deployment automation
  - Backup and restore scripts
  - Cron job configuration

---

## License

Copyright (c) 2026 Persistent Temp Mail Team. All rights reserved.
