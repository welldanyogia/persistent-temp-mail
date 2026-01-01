package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/attachment"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/config"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/events"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/logger"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/parser"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/smtp"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/ssl"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/storage"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize structured JSON logger
	appLogger := logger.New(logger.Config{
		Level:     cfg.Logging.Level,
		Format:    cfg.Logging.Format,
		Output:    cfg.Logging.Output,
		AddSource: cfg.Logging.AddSource,
	})

	// Set as default logger for slog
	slog.SetDefault(appLogger)

	appLogger.Info("Starting SMTP Server",
		slog.String("log_level", cfg.Logging.Level),
		slog.Int("smtp_port", cfg.SMTP.Port),
		slog.String("hostname", cfg.SMTP.Hostname),
	)

	// Setup database connection
	dbPool, err := setupDatabase(cfg, appLogger)
	if err != nil {
		appLogger.Error("Failed to connect to database", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer dbPool.Close()

	// Initialize storage service (for attachments)
	var storageService *storage.StorageService
	if cfg.Storage.AccessKeyID != "" && cfg.Storage.SecretAccessKey != "" {
		storageService, err = storage.NewStorageService(&cfg.Storage)
		if err != nil {
			appLogger.Warn("Failed to initialize storage service",
				slog.String("error", err.Error()),
			)
		} else {
			appLogger.Info("Storage service initialized",
				slog.String("bucket", cfg.Storage.Bucket),
			)
		}
	}

	// Create event store and bus for real-time notifications
	eventStore := events.NewEventStore(cfg.SSE.EventBufferSize)
	eventBus := events.NewEventBus(eventStore)

	// Initialize SSL service if enabled
	var sslService ssl.SSLService
	if cfg.SSL.Enabled {
		sslService = setupSSLService(cfg, dbPool, appLogger)
	}

	// Setup and start SMTP server
	smtpServer, err := setupSMTPServer(cfg, dbPool, storageService, eventBus, sslService, appLogger)
	if err != nil {
		appLogger.Error("Failed to initialize SMTP server", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if err := smtpServer.Start(); err != nil {
		appLogger.Error("Failed to start SMTP server", slog.String("error", err.Error()))
		os.Exit(1)
	}

	appLogger.Info("SMTP server started successfully",
		slog.Int("port", cfg.SMTP.Port),
		slog.String("hostname", cfg.SMTP.Hostname),
	)

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	appLogger.Info("Shutting down SMTP server...")

	if err := smtpServer.Stop(); err != nil {
		appLogger.Error("Error stopping SMTP server", slog.String("error", err.Error()))
		os.Exit(1)
	}

	appLogger.Info("SMTP server stopped gracefully")
}


// setupDatabase creates and configures the database connection pool
func setupDatabase(cfg *config.Config, log *slog.Logger) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	connString := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.DBName,
		cfg.Database.SSLMode,
	)

	poolConfig, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database config: %w", err)
	}

	poolConfig.MaxConns = 25
	poolConfig.MinConns = 5
	poolConfig.MaxConnLifetime = 5 * time.Minute
	poolConfig.MaxConnIdleTime = 1 * time.Minute
	poolConfig.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create database pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Info("Connected to database",
		slog.String("database", cfg.Database.DBName),
		slog.String("host", cfg.Database.Host),
	)
	return pool, nil
}

// setupSMTPServer creates and configures the SMTP server
func setupSMTPServer(cfg *config.Config, dbPool *pgxpool.Pool, storageService *storage.StorageService, eventBus *events.InMemoryEventBus, sslService ssl.SSLService, log *slog.Logger) (*smtp.SMTPServer, error) {
	smtpConfig := &smtp.SMTPConfig{
		Port:                cfg.SMTP.Port,
		Hostname:            cfg.SMTP.Hostname,
		MaxConnections:      cfg.SMTP.MaxConnections,
		MaxConnectionsPerIP: cfg.SMTP.MaxConnectionsPerIP,
		ConnectionTimeout:   cfg.SMTP.ConnectionTimeout,
		MaxMessageSize:      cfg.SMTP.MaxMessageSize,
		MaxRecipients:       cfg.SMTP.MaxRecipients,
		RateLimitPerMinute:  cfg.SMTP.RateLimitPerMinute,
	}

	// Setup TLS configuration
	var tlsConfig *tls.Config
	if sslService != nil {
		tlsConfig = sslService.GetTLSConfig()
		log.Info("SMTP using dynamic TLS configuration from SSL service")
	} else if cfg.SMTP.TLSEnabled && cfg.SMTP.TLSCertFile != "" && cfg.SMTP.TLSKeyFile != "" {
		var err error
		tlsConfig, err = smtp.LoadTLSConfig(cfg.SMTP.TLSCertFile, cfg.SMTP.TLSKeyFile)
		if err != nil {
			log.Warn("Failed to load TLS config - STARTTLS will be disabled",
				slog.String("error", err.Error()),
			)
		}
	}

	// Create alias repository adapter
	aliasRepo := smtp.NewPgxAliasRepository(dbPool)

	// Create SMTP server
	smtpServer := smtp.NewSMTPServer(smtpConfig, tlsConfig, aliasRepo)

	// Create email processor components
	emailParser := parser.NewEmailParser()

	// Create attachment handler if storage service is available
	var attachmentHandler *attachment.Handler
	if storageService != nil {
		attachmentHandler = attachment.NewHandler(
			storageService.GetClient(),
			storageService.GetBucket(),
		)
	}

	// Create repository adapters
	emailRepo := smtp.NewPgxEmailRepository(dbPool)
	attachmentRepo := smtp.NewPgxAttachmentRepository(dbPool)

	// Create event publisher
	var eventPublisher smtp.EventPublisher
	if eventBus != nil {
		eventPublisher = smtp.NewEventBusAdapter(eventBus)
	} else {
		eventPublisher = smtp.NewNoOpEventPublisher()
	}

	// Create processor
	stdLogger := slog.NewLogLogger(log.Handler(), slog.LevelInfo)
	processor := smtp.NewEmailProcessor(smtp.ProcessorConfig{
		Parser:            emailParser,
		AttachmentHandler: attachmentHandler,
		EmailRepo:         emailRepo,
		AttachmentRepo:    attachmentRepo,
		AliasRepo:         aliasRepo,
		EventPublisher:    eventPublisher,
		Logger:            stdLogger,
	})

	// Set the data callback
	smtpServer.SetDataCallback(func(ctx context.Context, data *smtp.DataResult) error {
		result, err := processor.ProcessEmail(ctx, data)
		if err != nil {
			log.Error("Error processing email", slog.String("error", err.Error()))
			return err
		}
		log.Info("Email processed successfully",
			slog.String("queue_id", result.QueueID),
			slog.String("email_id", result.EmailID),
			slog.Int("attachments", result.AttachmentCount),
		)
		return nil
	})

	return smtpServer, nil
}

// setupSSLService creates the SSL service for TLS certificates
func setupSSLService(cfg *config.Config, dbPool *pgxpool.Pool, log *slog.Logger) ssl.SSLService {
	encryptionKey := cfg.SSL.GetEncryptionKey()
	if encryptionKey == nil {
		log.Warn("SSL encryption key not configured")
		return nil
	}

	encryptedStore, err := ssl.NewEncryptedStore(cfg.SSL.CertStoragePath, encryptionKey)
	if err != nil {
		log.Warn("Failed to initialize encrypted store", slog.String("error", err.Error()))
		return nil
	}

	sslRepo := ssl.NewPostgresSSLCertificateRepository(dbPool)

	sslConfig := ssl.SSLServiceConfig{
		LetsEncryptEmail:   cfg.SSL.LetsEncryptEmail,
		LetsEncryptStaging: cfg.SSL.LetsEncryptStaging,
		CloudflareAPIToken: cfg.SSL.CloudflareAPIToken,
		CloudflareZoneID:   cfg.SSL.CloudflareZoneID,
		CertStoragePath:    cfg.SSL.CertStoragePath,
		CertEncryptionKey:  encryptionKey,
		RenewalDays:        cfg.SSL.CertRenewalDays,
		ProvisionTimeout:   cfg.SSL.ProvisionTimeout,
		MaxConcurrent:      cfg.SSL.MaxConcurrentProvision,
	}

	sslService, err := ssl.NewCertMagicService(sslConfig, encryptedStore, sslRepo)
	if err != nil {
		log.Warn("Failed to initialize SSL service", slog.String("error", err.Error()))
		return nil
	}

	// Load certificates into cache
	ctx := context.Background()
	if err := sslService.LoadAllCertificates(ctx); err != nil {
		log.Warn("Failed to load certificates", slog.String("error", err.Error()))
	}

	return sslService
}
