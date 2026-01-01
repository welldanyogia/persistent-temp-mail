package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver for sqlx
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/alias"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/api"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/attachment"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/auth"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/config"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/domain"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/email"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/events"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/health"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/logger"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/metrics"
	authmw "github.com/welldanyogia/persistent-temp-mail/backend/internal/middleware"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/parser"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/repository"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/sanitizer"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/smtp"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/sse"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/ssl"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/storage"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize structured JSON logger
	// Requirements: 14.3 - Application logs SHALL use structured JSON format
	// Requirements: 14.4 - Application logs SHALL include correlation IDs for request tracing
	// Requirements: 14.10 - System SHALL support log level configuration per environment
	appLogger := logger.New(logger.Config{
		Level:     cfg.Logging.Level,
		Format:    cfg.Logging.Format,
		Output:    cfg.Logging.Output,
		AddSource: cfg.Logging.AddSource,
	})

	// Set as default logger for slog
	slog.SetDefault(appLogger)

	// Log startup with configuration info
	appLogger.Info("Starting Persistent Temp Mail backend",
		slog.String("log_level", cfg.Logging.Level),
		slog.String("log_format", cfg.Logging.Format),
		slog.String("server_host", cfg.Server.Host),
		slog.String("server_port", cfg.Server.Port),
	)

	// Validate required configuration
	if cfg.JWT.AccessSecret == "" {
		appLogger.Error("JWT_ACCESS_SECRET environment variable is required")
		os.Exit(1)
	}
	if cfg.JWT.RefreshSecret == "" {
		appLogger.Error("JWT_REFRESH_SECRET environment variable is required")
		os.Exit(1)
	}

	// Setup database connection
	dbPool, err := setupDatabase(cfg, appLogger)
	if err != nil {
		appLogger.Error("Failed to connect to database", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer dbPool.Close()

	// Setup sqlx database connection for repositories that use sqlx
	// (email and attachment repositories)
	sqlxDB, err := setupSqlxDatabase(cfg, appLogger)
	if err != nil {
		appLogger.Error("Failed to connect to database with sqlx", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer sqlxDB.Close()

	// Initialize database metrics collector
	// Requirements: 9.5 - Add database connection metrics
	dbStatsCollector := metrics.NewDBStatsCollector(dbPool, sqlxDB.DB)
	dbStatsCollector.Start(15 * time.Second) // Collect stats every 15 seconds
	defer dbStatsCollector.Stop()

	// Setup Redis client (optional)
	// Requirements: 10.3 - Redis connectivity check in health endpoint
	var redisClient *redis.Client
	if cfg.Redis.Enabled {
		redisClient = setupRedis(cfg, appLogger)
		if redisClient != nil {
			appLogger.Info("Connected to Redis",
				slog.String("host", cfg.Redis.Host),
				slog.String("port", cfg.Redis.Port),
			)
			defer redisClient.Close()
		}
	} else {
		appLogger.Info("Redis is disabled (REDIS_ENABLED=false)")
	}

	// Initialize repositories
	userRepo := repository.NewUserRepository(dbPool)
	sessionRepo := repository.NewSessionRepository(dbPool)
	domainRepo := repository.NewDomainRepository(dbPool)
	aliasRepo := repository.NewAliasRepository(dbPool)

	// Initialize email and attachment repositories (using sqlx)
	// Requirements: All email inbox API requirements
	emailRepo := repository.NewEmailRepo(sqlxDB)
	attachmentRepo := repository.NewAttachmentRepository(sqlxDB)

	// Initialize storage service (for attachment cleanup)
	var storageService *storage.StorageService
	var orphanCleanupJob *storage.OrphanCleanupJob
	if cfg.Storage.AccessKeyID != "" && cfg.Storage.SecretAccessKey != "" {
		var err error
		storageService, err = storage.NewStorageService(&cfg.Storage)
		if err != nil {
			appLogger.Warn("Failed to initialize storage service",
				slog.String("error", err.Error()),
			)
			// Continue without storage service - attachment cleanup will be skipped
		} else {
			appLogger.Info("Storage service initialized",
				slog.String("bucket", cfg.Storage.Bucket),
			)

			// Initialize orphan cleanup job if enabled
			// Requirements: 4.7 - Run orphan cleanup job daily to remove unreferenced files
			if cfg.Storage.OrphanCleanupEnabled {
				cleanupConfig := storage.OrphanCleanupConfig{
					Interval:     cfg.Storage.OrphanCleanupInterval,
					AgeThreshold: cfg.Storage.OrphanCleanupAge,
					BatchSize:    1000,
					Enabled:      true,
				}
				orphanCleanupJob = storage.NewOrphanCleanupJob(storageService, attachmentRepo, cleanupConfig, nil)
				if err := orphanCleanupJob.Start(); err != nil {
					appLogger.Warn("Failed to start orphan cleanup job",
						slog.String("error", err.Error()),
					)
				} else {
					appLogger.Info("Orphan cleanup job started",
						slog.Duration("interval", cleanupConfig.Interval),
						slog.Duration("age_threshold", cleanupConfig.AgeThreshold),
					)
				}
			} else {
				appLogger.Info("Orphan cleanup job is disabled")
			}
		}
	} else {
		appLogger.Info("Storage service not configured - attachment cleanup will be skipped")
	}

	// Initialize services
	tokenService := auth.NewTokenService(auth.TokenServiceConfig{
		AccessSecret:       cfg.JWT.AccessSecret,
		RefreshSecret:      cfg.JWT.RefreshSecret,
		AccessTokenExpiry:  cfg.JWT.AccessTokenExpiry,
		RefreshTokenExpiry: cfg.JWT.RefreshTokenExpiry,
		Issuer:             cfg.JWT.Issuer,
	})

	passwordValidator := auth.NewPasswordValidator()

	authService := auth.NewAuthService(
		userRepo,
		sessionRepo,
		tokenService,
		passwordValidator,
	)

	// Initialize domain services
	dnsService := domain.NewDNSService(domain.DNSServiceConfig{
		MailServer: cfg.Domain.MailServer,
		TXTPrefix:  cfg.Domain.TXTPrefix,
		Logger:     appLogger,
	})

	domainSSLService := domain.NewSSLService(domain.SSLServiceConfig{
		Logger:  appLogger,
		Enabled: cfg.Domain.SSLEnabled,
	})

	domainService := domain.NewService(domain.ServiceConfig{
		Repository:  domainRepo,
		DNSService:  dnsService,
		SSLService:  domainSSLService,
		DomainLimit: cfg.Domain.DomainLimit,
		Logger:      appLogger,
	})

	// Initialize alias service
	// Requirements: All alias management
	aliasService := alias.NewService(alias.ServiceConfig{
		AliasRepository: aliasRepo,
		DomainRepo:      domainRepo,
		StorageService:  storageService,
		AliasLimit:      cfg.Alias.MaxAliasesPerUser,
		Logger:          appLogger,
	})

	// Initialize email service
	// Requirements: All email inbox API requirements (1.1-1.9, 2.1-2.8, 3.1-3.7, 4.1-4.5, 5.1-5.5, 6.1-6.5, 7.1-7.5)
	// Task 8.1: Wire all components together - Connect handlers → service → repositories → storage
	htmlSanitizer := sanitizer.NewHTMLSanitizer()
	baseURL := fmt.Sprintf("https://%s:%s/api/v1", cfg.Server.Host, cfg.Server.Port)
	if cfg.Server.Host == "0.0.0.0" || cfg.Server.Host == "localhost" {
		baseURL = fmt.Sprintf("http://localhost:%s/api/v1", cfg.Server.Port)
	}

	emailService := email.NewService(email.ServiceConfig{
		EmailRepo:      emailRepo,
		AttachmentRepo: attachmentRepo,
		StorageService: storageService,
		Sanitizer:      htmlSanitizer,
		Logger:         appLogger,
		BaseURL:        baseURL,
	})

	// Initialize SSE components for real-time notifications
	// Requirements: All realtime-notifications requirements
	// Task 11.1: Wire all components together - Connect SSE handler → connection manager → event bus
	sseConfig := sse.Config{
		HeartbeatInterval:     cfg.SSE.HeartbeatInterval,
		ConnectionTimeout:     cfg.SSE.ConnectionTimeout,
		MaxConnectionsPerUser: cfg.SSE.MaxConnectionsPerUser,
		EventBufferSize:       cfg.SSE.EventBufferSize,
	}

	// Create event store for replay functionality
	// Requirements: 7.3, 7.4 - Support Last-Event-ID for reconnection
	eventStore := events.NewEventStore(cfg.SSE.EventBufferSize)

	// Create event bus for publish/subscribe
	// Requirements: 3.1, 4.1, 5.1, 5.3, 6.1, 6.3 - Event publishing
	eventBus := events.NewEventBus(eventStore)

	// Create connection manager for SSE connections
	// Requirements: 1.5, 1.6, 8.2, 8.3 - Connection management
	connManager := sse.NewConnectionManager(sseConfig)

	// Start connection cleanup routine
	// Requirements: 2.3 - Detect dead connections and clean up resources
	stopCleanup := connManager.StartCleanupRoutine(sseConfig.HeartbeatInterval * 3)
	defer stopCleanup()

	// Create SSE handler
	// Requirements: 1.1, 1.2, 1.3, 1.4 - SSE connection handling
	sseHandler := sse.NewHandler(sseConfig, connManager, eventBus, tokenService)

	// Create event router for routing events to user connections
	// Requirements: 3.3 - Route events to correct user connections
	eventRouter := sse.NewEventRouter(connManager, eventBus)
	_ = eventRouter // Available for future use (e.g., manual event routing)

	appLogger.Info("SSE components initialized",
		slog.Duration("heartbeat", sseConfig.HeartbeatInterval),
		slog.Duration("timeout", sseConfig.ConnectionTimeout),
		slog.Int("max_connections", sseConfig.MaxConnectionsPerUser),
		slog.Int("buffer_size", sseConfig.EventBufferSize),
	)

	// Initialize SSL certificate management components
	// Requirements: 1.1, 3.1, 4.1 - SSL certificate provisioning, renewal, and STARTTLS
	var certMgmtService ssl.SSLService
	var renewalScheduler *ssl.RenewalScheduler
	if cfg.SSL.Enabled {
		certMgmtService, _, renewalScheduler = setupSSLService(cfg, dbPool, appLogger)
		if certMgmtService != nil {
			appLogger.Info("SSL certificate management initialized")
		}
	} else {
		appLogger.Info("SSL certificate management disabled (SSL_ENABLED=false)")
	}

	// Initialize handlers
	authHandler := auth.NewAuthHandler(authService)
	domainHandler := api.NewDomainHandler(domainService, appLogger)
	aliasHandler := alias.NewHandler(aliasService, appLogger)
	emailHandler := email.NewHandler(emailService, appLogger)

	// Initialize SSL handler if SSL is enabled
	// Requirements: 3.7 - SSL API endpoints
	var sslHandler *ssl.SSLHandler
	if certMgmtService != nil {
		sslHandler = ssl.NewSSLHandler(certMgmtService, domainRepo, appLogger)
	}

	// Initialize middleware
	authMiddleware := authmw.NewAuthMiddleware(tokenService)
	verifyRateLimiter := authmw.NewDomainVerifyRateLimiter()
	attachmentDownloadRateLimiter := authmw.NewAttachmentDownloadRateLimiter()

	// Setup router
	r := chi.NewRouter()

	// Global middleware
	// Requirements: 14.4 - Application logs SHALL include correlation IDs for request tracing
	r.Use(middleware.RequestID) // Generate request ID first
	r.Use(authmw.StructuredLogger(appLogger)) // Use structured JSON logging with correlation ID
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(middleware.Timeout(60 * time.Second))

	// Prometheus metrics middleware
	// Requirements: 9.5 - Backend SHALL expose /metrics endpoint in Prometheus format
	r.Use(metrics.Middleware)

	// CORS configuration
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://webrana.id", "https://www.webrana.id", "https://persistent-temp-mail.vercel.app", "http://localhost:3000"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-Request-ID"},
		ExposedHeaders:   []string{"Link", "X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Initialize health handler
	// Requirements: 10.1, 10.2, 10.3, 10.10, 10.11 - Enhanced health check endpoints
	healthHandler := health.NewHandler(health.Config{
		DBPool:      dbPool,
		RedisClient: redisClient,
		Version:     "1.0.0",
		Timeout:     5 * time.Second, // Requirement 10.10: Health check within 5 seconds
	})

	// Health check endpoints
	// Requirements: 10.1 - Backend SHALL expose /health endpoint returning 200 OK when healthy
	r.Get("/health", healthHandler.Health)

	// Readiness and liveness endpoints for Kubernetes/Docker
	// Requirements: 10.10, 10.11 - Readiness and liveness probes
	r.Get("/ready", healthHandler.Readiness)
	r.Get("/live", healthHandler.Liveness)

	// Prometheus metrics endpoint
	// Requirements: 9.5 - Backend SHALL expose /metrics endpoint in Prometheus format
	r.Handle("/metrics", metrics.Handler())

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
		// Register auth routes
		auth.RegisterRoutes(r, authHandler, authMiddleware.Authenticate)

		// Register domain routes with rate limiting for verify endpoint
		r.Route("/domains", func(r chi.Router) {
			r.Use(authMiddleware.Authenticate)

			r.Get("/", domainHandler.ListDomains)
			r.Post("/", domainHandler.CreateDomain)
			r.Get("/{id}", domainHandler.GetDomain)
			r.Delete("/{id}", domainHandler.DeleteDomain)
			r.Get("/{id}/dns-status", domainHandler.GetDNSStatus)

			// Verify endpoint with rate limiting
			r.With(verifyRateLimiter.RateLimitVerify).Post("/{id}/verify", domainHandler.VerifyDomain)

			// SSL certificate management endpoints
			// Requirements: 3.7 - SSL API endpoints for certificate management
			if sslHandler != nil {
				r.Get("/{id}/ssl", sslHandler.GetSSLStatus)
				r.Post("/{id}/ssl/provision", sslHandler.ProvisionSSL)
				r.Post("/{id}/ssl/renew", sslHandler.RenewSSL)
			}
		})

		// SSL health check endpoint (public)
		// Requirements: 8.6 - Provide API endpoint for SSL health check
		if sslHandler != nil {
			r.Get("/ssl/health", sslHandler.HealthCheck)
		}

		// Register alias routes
		// Requirements: All alias management endpoints
		alias.RegisterRoutes(r, aliasHandler, authMiddleware.Authenticate)

		// Register email routes with attachment download rate limiting
		// Requirements: All email inbox API endpoints (1.1-1.9, 2.1-2.8, 3.1-3.7, 4.1-4.5, 5.1-5.5, 6.1-6.5, 7.1-7.5)
		// Requirements: 6.7 - Rate limit downloads to 100 per user per hour
		// Task 8.1: Wire all components together
		email.RegisterRoutesWithRateLimit(r, emailHandler, authMiddleware.Authenticate, attachmentDownloadRateLimiter.RateLimitDownload)

		// Register SSE routes for real-time notifications
		// Requirements: 1.1, 1.2 - SSE Connection endpoint with authentication
		// Task 11.1: Wire all components together
		sse.RegisterRoutes(r, sseHandler)
	})

	// Create server
	addr := cfg.Server.Host + ":" + cfg.Server.Port
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		appLogger.Info("Starting HTTP server",
			slog.String("address", addr),
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			appLogger.Error("HTTP server failed",
				slog.String("error", err.Error()),
			)
			os.Exit(1)
		}
	}()

	// Initialize and start SMTP server
	// Requirements: All SMTP email receiver requirements
	// Task 11.1: Wire all components together
	var smtpServer *smtp.SMTPServer
	if cfg.SMTP.Port > 0 {
		smtpServer, err = setupSMTPServer(cfg, dbPool, storageService, eventBus, certMgmtService, appLogger)
		if err != nil {
			appLogger.Warn("Failed to initialize SMTP server",
				slog.String("error", err.Error()),
			)
			// Continue without SMTP server - email reception will be disabled
		} else {
			if err := smtpServer.Start(); err != nil {
				appLogger.Warn("Failed to start SMTP server",
					slog.String("error", err.Error()),
				)
			} else {
				appLogger.Info("SMTP server started",
					slog.Int("port", cfg.SMTP.Port),
				)
			}
		}
	} else {
		appLogger.Info("SMTP server disabled (SMTP_PORT not configured)")
	}

	// Register SMTP health endpoint after SMTP server is initialized
	// Requirements: 10.5 - SMTP health check endpoint
	smtpHealthHandler := health.NewSMTPHandler(health.SMTPHandlerConfig{
		SMTPServer:  smtpServer,
		EHLOChecker: smtpServer,
		Hostname:    cfg.SMTP.Hostname,
		Port:        cfg.SMTP.Port,
		Timeout:     5 * time.Second,
	})
	r.Get("/smtp/health", smtpHealthHandler.SMTPHealth)

	// Graceful shutdown
	// Requirements: 10.11 - Implement graceful shutdown handling (SIGTERM)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	appLogger.Info("Shutting down servers...")

	// Set health handler to not ready (for load balancer drain)
	// Requirements: 10.11 - Graceful shutdown handling
	healthHandler.SetReady(false)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop orphan cleanup job first
	if orphanCleanupJob != nil {
		orphanCleanupJob.Stop()
	}

	// Stop SSL renewal scheduler
	if renewalScheduler != nil {
		renewalScheduler.Stop()
	}

	// Stop SMTP server first
	if smtpServer != nil {
		if err := smtpServer.Stop(); err != nil {
			appLogger.Error("Error stopping SMTP server",
				slog.String("error", err.Error()),
			)
		}
	}

	if err := srv.Shutdown(ctx); err != nil {
		appLogger.Error("HTTP server forced to shutdown",
			slog.String("error", err.Error()),
		)
		os.Exit(1)
	}

	appLogger.Info("Servers exited gracefully")
}

// setupDatabase creates and configures the database connection pool
func setupDatabase(cfg *config.Config, log *slog.Logger) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Build connection string
	connString := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.DBName,
		cfg.Database.SSLMode,
	)

	// Configure pool
	poolConfig, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database config: %w", err)
	}

	// Set pool configuration
	poolConfig.MaxConns = 50
	poolConfig.MinConns = 5
	poolConfig.MaxConnLifetime = 5 * time.Minute
	poolConfig.MaxConnIdleTime = 1 * time.Minute
	poolConfig.HealthCheckPeriod = 30 * time.Second

	// Create pool
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create database pool: %w", err)
	}

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Info("Connected to database",
		slog.String("database", cfg.Database.DBName),
		slog.String("host", cfg.Database.Host),
		slog.String("port", cfg.Database.Port),
	)
	return pool, nil
}

// setupSMTPServer creates and configures the SMTP server with all components wired together
// Requirements: All SMTP email receiver requirements
// Task 11.1: Wire all components together - Connect SMTP server → parser → attachment handler → repositories → event bus
func setupSMTPServer(cfg *config.Config, dbPool *pgxpool.Pool, storageService *storage.StorageService, eventBus *events.InMemoryEventBus, sslService ssl.SSLService, log *slog.Logger) (*smtp.SMTPServer, error) {
	// Create SMTP configuration from app config
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

	// Setup TLS configuration if enabled
	// Requirements: 1.2, 1.3 - Support STARTTLS with TLS 1.2+
	// Requirements: 4.1, 4.6, 4.7 - Use SSLService for dynamic TLS config
	var tlsConfig *tls.Config
	if sslService != nil {
		// Use dynamic TLS config from SSL service for multi-domain support
		// Requirements: 4.7 - Support SNI for multi-domain
		tlsConfig = sslService.GetTLSConfig()
		log.Info("SMTP using dynamic TLS configuration from SSL service")
	} else if cfg.SMTP.TLSEnabled && cfg.SMTP.TLSCertFile != "" && cfg.SMTP.TLSKeyFile != "" {
		// Fallback to static TLS config from files
		var err error
		tlsConfig, err = smtp.LoadTLSConfig(cfg.SMTP.TLSCertFile, cfg.SMTP.TLSKeyFile)
		if err != nil {
			log.Warn("Failed to load TLS config - STARTTLS will be disabled",
				slog.String("error", err.Error()),
			)
		} else {
			log.Info("SMTP TLS configuration loaded from files")
		}
	}

	// Create alias repository adapter for SMTP server
	// Requirements: 2.1-2.5 - Recipient validation
	aliasRepo := smtp.NewPgxAliasRepository(dbPool)

	// Create SMTP server
	smtpServer := smtp.NewSMTPServer(smtpConfig, tlsConfig, aliasRepo)

	// Create email processor components
	// Requirements: 4.1-4.12 - Email parsing
	emailParser := parser.NewEmailParser()

	// Create attachment handler if storage service is available
	// Requirements: 5.1-5.10 - Attachment handling
	var attachmentHandler *attachment.Handler
	if storageService != nil {
		attachmentHandler = attachment.NewHandler(
			storageService.GetClient(),
			storageService.GetBucket(),
		)
		log.Info("SMTP attachment handler initialized")
	} else {
		log.Info("SMTP attachment handler disabled (storage service not configured)")
	}

	// Create repository adapters for email processor
	emailRepo := smtp.NewPgxEmailRepository(dbPool)
	attachmentRepo := smtp.NewPgxAttachmentRepository(dbPool)

	// Create email processor
	// Requirements: All - Wire SMTP → parser → attachment handler → repositories → event bus
	// Task 11.1: Wire event bus for real-time notifications
	var eventPublisher smtp.EventPublisher
	if eventBus != nil {
		eventPublisher = smtp.NewEventBusAdapter(eventBus)
		log.Info("SMTP event publisher connected to SSE event bus")
	} else {
		eventPublisher = smtp.NewNoOpEventPublisher()
		log.Info("SMTP event publisher using no-op (SSE disabled)")
	}

	// Create a standard log.Logger for the processor (it expects *log.Logger)
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

	// Set the data callback on the SMTP server to process emails
	// This wires: SMTP server → parser → attachment handler → repositories
	smtpServer.SetDataCallback(func(ctx context.Context, data *smtp.DataResult) error {
		result, err := processor.ProcessEmail(ctx, data)
		if err != nil {
			log.Error("Error processing email",
				slog.String("error", err.Error()),
			)
			return err
		}
		if len(result.Errors) > 0 {
			log.Warn("Email processed with errors",
				slog.Any("errors", result.Errors),
			)
		}
		log.Info("Email processed successfully",
			slog.String("queue_id", result.QueueID),
			slog.String("email_id", result.EmailID),
			slog.Int("attachments", result.AttachmentCount),
		)
		return nil
	})

	log.Info("SMTP server configured",
		slog.Int("port", smtpConfig.Port),
		slog.String("hostname", smtpConfig.Hostname),
		slog.Int("max_connections", smtpConfig.MaxConnections),
		slog.Bool("tls_enabled", tlsConfig != nil),
	)

	return smtpServer, nil
}

// setupSqlxDatabase creates and configures a sqlx database connection
// This is used by repositories that require sqlx (email, attachment)
func setupSqlxDatabase(cfg *config.Config, log *slog.Logger) (*sqlx.DB, error) {
	// Build connection string
	connString := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.DBName,
		cfg.Database.SSLMode,
	)

	// Open connection
	db, err := sqlx.Connect("pgx", connString)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database with sqlx: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Info("Connected to database with sqlx",
		slog.String("database", cfg.Database.DBName),
		slog.String("host", cfg.Database.Host),
		slog.String("port", cfg.Database.Port),
	)
	return db, nil
}

// setupSSLService creates and configures the SSL certificate management service
// Requirements: 1.1, 3.1, 4.1 - SSL certificate provisioning, renewal, and STARTTLS
func setupSSLService(cfg *config.Config, dbPool *pgxpool.Pool, log *slog.Logger) (ssl.SSLService, ssl.SSLCertificateRepository, *ssl.RenewalScheduler) {
	// Validate required configuration
	encryptionKey := cfg.SSL.GetEncryptionKey()
	if encryptionKey == nil {
		log.Warn("SSL encryption key not configured or invalid (CERT_ENCRYPTION_KEY must be 64 hex characters)")
		log.Info("SSL certificate management will be disabled")
		return nil, nil, nil
	}

	// Initialize encrypted certificate store
	// Requirements: 2.1, 2.6 - Encrypted certificate storage
	encryptedStore, err := ssl.NewEncryptedStore(cfg.SSL.CertStoragePath, encryptionKey)
	if err != nil {
		log.Warn("Failed to initialize encrypted store",
			slog.String("error", err.Error()),
		)
		log.Info("SSL certificate management will be disabled")
		return nil, nil, nil
	}
	log.Info("SSL encrypted store initialized",
		slog.String("path", cfg.SSL.CertStoragePath),
	)

	// Initialize SSL certificate repository
	// Requirements: 2.4 - Store certificate metadata in database
	sslRepo := ssl.NewPostgresSSLCertificateRepository(dbPool)

	// Create SSL service configuration
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

	// Initialize CertMagic service
	// Requirements: 1.2, 1.3, 1.4, 1.5 - CertMagic integration with Let's Encrypt
	sslService, err := ssl.NewCertMagicService(sslConfig, encryptedStore, sslRepo)
	if err != nil {
		log.Warn("Failed to initialize SSL service",
			slog.String("error", err.Error()),
		)
		log.Info("SSL certificate management will be disabled")
		return nil, nil, nil
	}

	// Load all active certificates into cache on startup
	// Requirements: 2.7 - Load certificates into memory on server startup
	ctx := context.Background()
	if err := sslService.LoadAllCertificates(ctx); err != nil {
		log.Warn("Failed to load certificates into cache",
			slog.String("error", err.Error()),
		)
		// Continue anyway - certificates will be loaded on demand
	}

	// Initialize renewal scheduler
	// Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.8 - Certificate renewal
	schedulerConfig := ssl.RenewalSchedulerConfig{
		CheckInterval:         cfg.SSL.CertCheckInterval,
		RenewalDays:           cfg.SSL.CertRenewalDays,
		AlertDays:             []int{14, 7, 3, 1},
		RetryInterval:         24 * time.Hour,
		MaxConcurrentRenewals: 5,
	}

	// Use no-op notification service for now
	// In production, this would be replaced with an actual notification service
	notifier := &ssl.NoOpNotificationService{}

	renewalScheduler := ssl.NewRenewalScheduler(sslService, sslRepo, notifier, schedulerConfig)

	// Start renewal scheduler in background
	go renewalScheduler.Start(ctx)

	log.Info("SSL service initialized",
		slog.String("email", cfg.SSL.LetsEncryptEmail),
		slog.Bool("staging", cfg.SSL.LetsEncryptStaging),
		slog.Int("renewal_days", cfg.SSL.CertRenewalDays),
		slog.Duration("check_interval", cfg.SSL.CertCheckInterval),
	)

	return sslService, sslRepo, renewalScheduler
}


// setupRedis creates and configures the Redis client
// Requirements: 10.3 - Redis connectivity check in health endpoint
func setupRedis(cfg *config.Config, log *slog.Logger) *redis.Client {
	if !cfg.Redis.Enabled {
		return nil
	}

	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", cfg.Redis.Host, cfg.Redis.Port),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := client.Ping(ctx).Result(); err != nil {
		log.Warn("Failed to connect to Redis",
			slog.String("error", err.Error()),
		)
		client.Close()
		return nil
	}

	return client
}
