# Certbot SSL Certificate Configuration

This directory contains the Let's Encrypt certificate configuration and renewal hooks for the Persistent Temp Mail service.

## Directory Structure

```
certbot/
├── cli.ini                    # Certbot CLI configuration
├── conf/                      # Let's Encrypt configuration (auto-generated)
│   ├── live/                  # Active certificates
│   │   ├── webrana.id/
│   │   ├── api.webrana.id/
│   │   └── mail.webrana.id/
│   ├── archive/               # Certificate history
│   └── renewal/               # Renewal configuration
├── www/                       # Webroot for ACME challenges
│   └── .well-known/
│       └── acme-challenge/
└── renewal-hooks/             # Renewal hook scripts
    ├── pre/                   # Pre-renewal hooks
    │   └── check-nginx.sh
    ├── deploy/                # Deploy hooks (run on successful renewal)
    │   └── reload-nginx.sh
    └── post/                  # Post-renewal hooks
        └── notify-renewal.sh
```

## Initial Certificate Setup

### 1. Ensure Nginx is running with HTTP challenge support

The Nginx configuration must include the ACME challenge location:

```nginx
location /.well-known/acme-challenge/ {
    root /var/www/certbot;
}
```

### 2. Issue certificates for all domains

```bash
# Using the ssl-renew.sh script
./scripts/ssl-renew.sh setup

# Or manually with Docker
docker run --rm \
    -v ./certbot/conf:/etc/letsencrypt \
    -v ./certbot/www:/var/www/certbot \
    certbot/certbot certonly \
    --webroot \
    --webroot-path=/var/www/certbot \
    --email admin@webrana.id \
    --agree-tos \
    --no-eff-email \
    -d webrana.id \
    -d www.webrana.id

# Repeat for other domains
docker run --rm \
    -v ./certbot/conf:/etc/letsencrypt \
    -v ./certbot/www:/var/www/certbot \
    certbot/certbot certonly \
    --webroot \
    --webroot-path=/var/www/certbot \
    --email admin@webrana.id \
    --agree-tos \
    --no-eff-email \
    -d api.webrana.id

docker run --rm \
    -v ./certbot/conf:/etc/letsencrypt \
    -v ./certbot/www:/var/www/certbot \
    certbot/certbot certonly \
    --webroot \
    --webroot-path=/var/www/certbot \
    --email admin@webrana.id \
    --agree-tos \
    --no-eff-email \
    -d mail.webrana.id
```

### 3. Restart Nginx to load certificates

```bash
docker compose -f docker-compose.prod.yml restart nginx
```

## Automatic Renewal

Certificates are automatically renewed by the certbot container which runs a renewal check every 12 hours. Let's Encrypt certificates are valid for 90 days, and certbot renews them when they have less than 30 days until expiry.

### Renewal Methods

1. **Docker Container (Recommended)**
   The `ptm-certbot` container in `docker-compose.prod.yml` handles automatic renewal.

2. **Cron Job**
   Install the cron configuration:
   ```bash
   sudo cp scripts/cron/ssl-renewal-cron /etc/cron.d/ptm-ssl-renewal
   sudo chmod 644 /etc/cron.d/ptm-ssl-renewal
   ```

3. **Manual Renewal**
   ```bash
   ./scripts/ssl-renew.sh renew
   ```

## Renewal Hooks

### Pre-Renewal Hook (`check-nginx.sh`)
- Verifies Nginx is running before renewal
- Ensures webroot directory exists
- Creates ACME challenge directory if needed

### Deploy Hook (`reload-nginx.sh`)
- Runs after successful certificate renewal
- Tests Nginx configuration
- Gracefully reloads Nginx to load new certificates

### Post-Renewal Hook (`notify-renewal.sh`)
- Logs renewal completion
- Checks certificate expiry dates
- Can send notifications (Slack, email) - configure as needed

## Monitoring Certificate Status

### Check expiry dates
```bash
./scripts/ssl-renew.sh expiry
```

### View certificate details
```bash
./scripts/ssl-renew.sh check
```

### Test renewal (dry-run)
```bash
./scripts/ssl-renew.sh dry-run
```

## Troubleshooting

### Certificate not renewing
1. Check certbot logs: `docker logs ptm-certbot`
2. Verify webroot is accessible: `curl http://webrana.id/.well-known/acme-challenge/test`
3. Check DNS resolution: `dig webrana.id`

### Nginx not reloading
1. Check Nginx configuration: `docker exec ptm-nginx nginx -t`
2. Manually reload: `docker exec ptm-nginx nginx -s reload`

### Rate limits
Let's Encrypt has rate limits:
- 50 certificates per registered domain per week
- 5 duplicate certificates per week
- 5 failed validations per hour

Use `--dry-run` for testing to avoid hitting rate limits.

## Security Notes

- Certificate private keys are stored in `certbot/conf/live/*/privkey.pem`
- Keep the `certbot/conf` directory secure (mode 700)
- Never commit certificates or private keys to version control
- The `conf` directory is in `.gitignore`

## References

- [Let's Encrypt Documentation](https://letsencrypt.org/docs/)
- [Certbot Documentation](https://certbot.eff.org/docs/)
- [ACME Protocol](https://tools.ietf.org/html/rfc8555)
