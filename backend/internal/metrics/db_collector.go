// Package metrics provides Prometheus metrics for the backend API
package metrics

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DBStatsCollector collects database connection statistics
type DBStatsCollector struct {
	pgxPool *pgxpool.Pool
	sqlxDB  *sql.DB
	stopCh  chan struct{}
}

// NewDBStatsCollector creates a new database stats collector
func NewDBStatsCollector(pgxPool *pgxpool.Pool, sqlxDB *sql.DB) *DBStatsCollector {
	return &DBStatsCollector{
		pgxPool: pgxPool,
		sqlxDB:  sqlxDB,
		stopCh:  make(chan struct{}),
	}
}

// Start begins collecting database statistics at regular intervals
func (c *DBStatsCollector) Start(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Collect initial stats
		c.collect()

		for {
			select {
			case <-ticker.C:
				c.collect()
			case <-c.stopCh:
				return
			}
		}
	}()

	log.Printf("Database stats collector started with interval: %v", interval)
}

// Stop stops the database stats collector
func (c *DBStatsCollector) Stop() {
	close(c.stopCh)
	log.Println("Database stats collector stopped")
}

// collect gathers database statistics and updates Prometheus metrics
func (c *DBStatsCollector) collect() {
	// Collect pgxpool stats
	if c.pgxPool != nil {
		stat := c.pgxPool.Stat()
		DBConnectionsOpen.Set(float64(stat.TotalConns()))
		DBConnectionsInUse.Set(float64(stat.AcquiredConns()))
		DBConnectionsIdle.Set(float64(stat.IdleConns()))
		DBConnectionsMaxOpen.Set(float64(stat.MaxConns()))
	}

	// Collect sqlx/sql.DB stats (if available)
	if c.sqlxDB != nil {
		stats := c.sqlxDB.Stats()
		// These will override pgxpool stats if both are set
		// In practice, they should be similar since they connect to the same DB
		DBConnectionsOpen.Set(float64(stats.OpenConnections))
		DBConnectionsInUse.Set(float64(stats.InUse))
		DBConnectionsIdle.Set(float64(stats.Idle))
		DBConnectionsMaxOpen.Set(float64(stats.MaxOpenConnections))
	}
}

// RecordQueryDuration records the duration of a database query
func RecordQueryDuration(operation string, duration time.Duration) {
	DBQueryDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

// TimeQuery is a helper function to time database queries
// Usage: defer metrics.TimeQuery("select_user")()
func TimeQuery(operation string) func() {
	start := time.Now()
	return func() {
		RecordQueryDuration(operation, time.Since(start))
	}
}

// PingDatabase checks database connectivity and records the result
func PingDatabase(ctx context.Context, pool *pgxpool.Pool) error {
	start := time.Now()
	err := pool.Ping(ctx)
	RecordQueryDuration("ping", time.Since(start))
	return err
}
