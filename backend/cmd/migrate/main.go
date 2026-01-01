// Package main provides a CLI tool for database migrations.
// Requirements: 11.1, 11.2, 11.3, 11.4, 11.5, 11.6, 11.7, 11.8
// - Uses golang-migrate for migration management
// - Stores migrations in /backend/migrations directory
// - Supports up and down migrations via CLI
// - Tracks migration version in schema_migrations table
// - Prevents duplicate migration execution
// - Supports timeout configuration (5 minutes per migration)
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Version is set at build time
var Version = "dev"

// Default configuration values
const (
	defaultMigrationTimeout = 5 * time.Minute
	defaultMigrationsPath   = "migrations"
)

// Config holds migration configuration
type Config struct {
	DatabaseURL    string
	MigrationsPath string
	Timeout        time.Duration
	DryRun         bool
}

func main() {
	// Parse command line flags
	var (
		dbHost     = flag.String("db-host", getEnv("DB_HOST", "localhost"), "Database host")
		dbPort     = flag.String("db-port", getEnv("DB_PORT", "5432"), "Database port")
		dbUser     = flag.String("db-user", getEnv("DB_USER", "postgres"), "Database user")
		dbPassword = flag.String("db-password", getEnv("DB_PASSWORD", ""), "Database password")
		dbName     = flag.String("db-name", getEnv("DB_NAME", "persistent_temp_mail"), "Database name")
		dbSSLMode  = flag.String("db-sslmode", getEnv("DB_SSLMODE", "disable"), "Database SSL mode")
		migrPath   = flag.String("path", getEnv("MIGRATIONS_PATH", defaultMigrationsPath), "Path to migrations directory")
		timeout    = flag.Duration("timeout", defaultMigrationTimeout, "Timeout per migration")
		dryRun     = flag.Bool("dry-run", false, "Show what would be done without executing")
		version    = flag.Bool("version", false, "Print version and exit")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <command> [args]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Database Migration Tool for Persistent Temp Mail\n\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  up [N]       Apply all or N up migrations\n")
		fmt.Fprintf(os.Stderr, "  down [N]     Apply all or N down migrations\n")
		fmt.Fprintf(os.Stderr, "  goto V       Migrate to version V\n")
		fmt.Fprintf(os.Stderr, "  force V      Set version V without running migrations (use with caution)\n")
		fmt.Fprintf(os.Stderr, "  version      Print current migration version\n")
		fmt.Fprintf(os.Stderr, "  drop         Drop all tables (use with extreme caution)\n")
		fmt.Fprintf(os.Stderr, "  create NAME  Create a new migration file pair\n")
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEnvironment Variables:\n")
		fmt.Fprintf(os.Stderr, "  DB_HOST          Database host (default: localhost)\n")
		fmt.Fprintf(os.Stderr, "  DB_PORT          Database port (default: 5432)\n")
		fmt.Fprintf(os.Stderr, "  DB_USER          Database user (default: postgres)\n")
		fmt.Fprintf(os.Stderr, "  DB_PASSWORD      Database password\n")
		fmt.Fprintf(os.Stderr, "  DB_NAME          Database name (default: persistent_temp_mail)\n")
		fmt.Fprintf(os.Stderr, "  DB_SSLMODE       Database SSL mode (default: disable)\n")
		fmt.Fprintf(os.Stderr, "  MIGRATIONS_PATH  Path to migrations directory (default: migrations)\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s up                    # Apply all pending migrations\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s up 1                  # Apply next migration\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s down 1                # Rollback last migration\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s version               # Show current version\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s create add_indexes    # Create new migration files\n", os.Args[0])
	}

	flag.Parse()

	if *version {
		fmt.Printf("migrate version %s\n", Version)
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	// Build database URL
	dbURL := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		*dbUser, *dbPassword, *dbHost, *dbPort, *dbName, *dbSSLMode)

	cfg := &Config{
		DatabaseURL:    dbURL,
		MigrationsPath: *migrPath,
		Timeout:        *timeout,
		DryRun:         *dryRun,
	}

	// Execute command
	cmd := args[0]
	cmdArgs := args[1:]

	if err := runCommand(cfg, cmd, cmdArgs); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

// runCommand executes the specified migration command
func runCommand(cfg *Config, cmd string, args []string) error {
	switch cmd {
	case "create":
		if len(args) < 1 {
			return fmt.Errorf("create requires a migration name")
		}
		return createMigration(cfg, args[0])
	case "version":
		return showVersion(cfg)
	case "up":
		steps := 0
		if len(args) > 0 {
			if _, err := fmt.Sscanf(args[0], "%d", &steps); err != nil {
				return fmt.Errorf("invalid number of steps: %s", args[0])
			}
		}
		return migrateUp(cfg, steps)
	case "down":
		steps := 0
		if len(args) > 0 {
			if _, err := fmt.Sscanf(args[0], "%d", &steps); err != nil {
				return fmt.Errorf("invalid number of steps: %s", args[0])
			}
		}
		return migrateDown(cfg, steps)
	case "goto":
		if len(args) < 1 {
			return fmt.Errorf("goto requires a version number")
		}
		var version uint
		if _, err := fmt.Sscanf(args[0], "%d", &version); err != nil {
			return fmt.Errorf("invalid version: %s", args[0])
		}
		return migrateGoto(cfg, version)
	case "force":
		if len(args) < 1 {
			return fmt.Errorf("force requires a version number")
		}
		var version int
		if _, err := fmt.Sscanf(args[0], "%d", &version); err != nil {
			return fmt.Errorf("invalid version: %s", args[0])
		}
		return migrateForce(cfg, version)
	case "drop":
		return migrateDrop(cfg)
	default:
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

// createMigration creates a new migration file pair
func createMigration(cfg *Config, name string) error {
	// Find the next migration number
	nextNum, err := getNextMigrationNumber(cfg.MigrationsPath)
	if err != nil {
		return fmt.Errorf("failed to determine next migration number: %w", err)
	}

	// Create migration files
	upFile := filepath.Join(cfg.MigrationsPath, fmt.Sprintf("%03d_%s.up.sql", nextNum, name))
	downFile := filepath.Join(cfg.MigrationsPath, fmt.Sprintf("%03d_%s.down.sql", nextNum, name))

	if cfg.DryRun {
		log.Printf("[DRY RUN] Would create: %s", upFile)
		log.Printf("[DRY RUN] Would create: %s", downFile)
		return nil
	}

	// Create migrations directory if it doesn't exist
	if err := os.MkdirAll(cfg.MigrationsPath, 0755); err != nil {
		return fmt.Errorf("failed to create migrations directory: %w", err)
	}

	// Create up migration file
	upContent := fmt.Sprintf("-- Migration: %s\n-- Created: %s\n\n-- Add your UP migration SQL here\n",
		name, time.Now().Format(time.RFC3339))
	if err := os.WriteFile(upFile, []byte(upContent), 0644); err != nil {
		return fmt.Errorf("failed to create up migration: %w", err)
	}

	// Create down migration file
	downContent := fmt.Sprintf("-- Migration: %s (rollback)\n-- Created: %s\n\n-- Add your DOWN migration SQL here\n",
		name, time.Now().Format(time.RFC3339))
	if err := os.WriteFile(downFile, []byte(downContent), 0644); err != nil {
		return fmt.Errorf("failed to create down migration: %w", err)
	}

	log.Printf("Created migration files:")
	log.Printf("  %s", upFile)
	log.Printf("  %s", downFile)

	return nil
}

// getNextMigrationNumber finds the next available migration number
func getNextMigrationNumber(migrationsPath string) (int, error) {
	entries, err := os.ReadDir(migrationsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 1, nil
		}
		return 0, err
	}

	maxNum := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		var num int
		if _, err := fmt.Sscanf(entry.Name(), "%d_", &num); err == nil {
			if num > maxNum {
				maxNum = num
			}
		}
	}

	return maxNum + 1, nil
}

// showVersion displays the current migration version
func showVersion(cfg *Config) error {
	m, err := newMigrate(cfg)
	if err != nil {
		return err
	}
	defer m.Close()

	version, dirty, err := m.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			log.Println("No migrations have been applied yet")
			return nil
		}
		return fmt.Errorf("failed to get version: %w", err)
	}

	status := ""
	if dirty {
		status = " (dirty)"
	}
	log.Printf("Current migration version: %d%s", version, status)

	return nil
}

// migrateUp applies up migrations
func migrateUp(cfg *Config, steps int) error {
	if cfg.DryRun {
		log.Printf("[DRY RUN] Would apply %d up migrations (0 = all)", steps)
		return nil
	}

	m, err := newMigrate(cfg)
	if err != nil {
		return err
	}
	defer m.Close()

	// Get current version for logging
	currentVersion, _, _ := m.Version()

	log.Printf("Starting migration up from version %d...", currentVersion)

	if steps > 0 {
		err = m.Steps(steps)
	} else {
		err = m.Up()
	}

	if err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			log.Println("No migrations to apply")
			return nil
		}
		return fmt.Errorf("migration failed: %w", err)
	}

	// Get new version
	newVersion, _, _ := m.Version()
	log.Printf("Migration completed: %d -> %d", currentVersion, newVersion)

	return nil
}

// migrateDown applies down migrations
func migrateDown(cfg *Config, steps int) error {
	if cfg.DryRun {
		log.Printf("[DRY RUN] Would apply %d down migrations (0 = all)", steps)
		return nil
	}

	m, err := newMigrate(cfg)
	if err != nil {
		return err
	}
	defer m.Close()

	// Get current version for logging
	currentVersion, _, _ := m.Version()

	log.Printf("Starting migration down from version %d...", currentVersion)

	if steps > 0 {
		err = m.Steps(-steps)
	} else {
		err = m.Down()
	}

	if err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			log.Println("No migrations to rollback")
			return nil
		}
		return fmt.Errorf("migration failed: %w", err)
	}

	// Get new version
	newVersion, _, _ := m.Version()
	log.Printf("Migration completed: %d -> %d", currentVersion, newVersion)

	return nil
}

// migrateGoto migrates to a specific version
func migrateGoto(cfg *Config, version uint) error {
	if cfg.DryRun {
		log.Printf("[DRY RUN] Would migrate to version %d", version)
		return nil
	}

	m, err := newMigrate(cfg)
	if err != nil {
		return err
	}
	defer m.Close()

	// Get current version for logging
	currentVersion, _, _ := m.Version()

	log.Printf("Migrating from version %d to %d...", currentVersion, version)

	if err := m.Migrate(version); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			log.Printf("Already at version %d", version)
			return nil
		}
		return fmt.Errorf("migration failed: %w", err)
	}

	log.Printf("Migration completed: %d -> %d", currentVersion, version)

	return nil
}

// migrateForce sets the version without running migrations
func migrateForce(cfg *Config, version int) error {
	if cfg.DryRun {
		log.Printf("[DRY RUN] Would force version to %d", version)
		return nil
	}

	m, err := newMigrate(cfg)
	if err != nil {
		return err
	}
	defer m.Close()

	log.Printf("Forcing version to %d (no migrations will be run)...", version)

	if err := m.Force(version); err != nil {
		return fmt.Errorf("force failed: %w", err)
	}

	log.Printf("Version forced to %d", version)

	return nil
}

// migrateDrop drops all tables
func migrateDrop(cfg *Config) error {
	if cfg.DryRun {
		log.Println("[DRY RUN] Would drop all tables")
		return nil
	}

	// Confirm dangerous operation
	log.Println("WARNING: This will drop ALL tables in the database!")
	log.Println("Type 'yes' to confirm:")

	var confirm string
	if _, err := fmt.Scanln(&confirm); err != nil || confirm != "yes" {
		log.Println("Aborted")
		return nil
	}

	m, err := newMigrate(cfg)
	if err != nil {
		return err
	}
	defer m.Close()

	log.Println("Dropping all tables...")

	if err := m.Drop(); err != nil {
		return fmt.Errorf("drop failed: %w", err)
	}

	log.Println("All tables dropped")

	return nil
}

// newMigrate creates a new migrate instance with timeout context
func newMigrate(cfg *Config) (*migrate.Migrate, error) {
	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	// Open database connection
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection with timeout
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Create postgres driver instance
	driver, err := postgres.WithInstance(db, &postgres.Config{
		MigrationsTable: "schema_migrations",
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create database driver: %w", err)
	}

	// Get absolute path to migrations
	migrationsPath, err := filepath.Abs(cfg.MigrationsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve migrations path: %w", err)
	}

	// Create migrate instance
	sourceURL := fmt.Sprintf("file://%s", migrationsPath)
	m, err := migrate.NewWithDatabaseInstance(sourceURL, "postgres", driver)
	if err != nil {
		return nil, fmt.Errorf("failed to create migrate instance: %w", err)
	}

	// Set lock timeout
	m.LockTimeout = cfg.Timeout

	return m, nil
}

// getEnv returns environment variable value or default
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
