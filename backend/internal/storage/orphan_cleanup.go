package storage

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// OrphanCleanupConfig holds configuration for the orphan cleanup job
// Requirements: 4.7 - Run orphan cleanup job daily to remove unreferenced files
type OrphanCleanupConfig struct {
	Interval time.Duration // Interval between cleanup runs (default: 24 hours)
	AgeThreshold time.Duration // Age threshold for orphan files (default: 7 days)
	BatchSize int // Number of files to process per batch (default: 1000)
	Enabled bool // Whether cleanup is enabled
}

// DefaultOrphanCleanupConfig returns default configuration
func DefaultOrphanCleanupConfig() OrphanCleanupConfig {
	return OrphanCleanupConfig{
		Interval:     24 * time.Hour,
		AgeThreshold: 7 * 24 * time.Hour, // 7 days
		BatchSize:    1000,
		Enabled:      true,
	}
}

// StorageKeyChecker is an interface for checking if a storage key exists in the database
type StorageKeyChecker interface {
	// ExistsInDatabase checks if the given storage key exists in the attachments table
	ExistsInDatabase(ctx context.Context, storageKey string) (bool, error)
	// BatchExistsInDatabase checks multiple storage keys and returns those that exist
	BatchExistsInDatabase(ctx context.Context, storageKeys []string) (map[string]bool, error)
}

// OrphanCleanupJob handles periodic cleanup of orphaned files in storage
// Requirements: 4.7 - Run orphan cleanup job daily to remove unreferenced files
type OrphanCleanupJob struct {
	storage      *StorageService
	keyChecker   StorageKeyChecker
	config       OrphanCleanupConfig
	logger       *log.Logger
	stopChan     chan struct{}
	wg           sync.WaitGroup
	mu           sync.Mutex
	running      bool
	lastRun      time.Time
	lastResult   *CleanupResult
}

// CleanupResult holds the result of a cleanup run
type CleanupResult struct {
	StartTime      time.Time
	EndTime        time.Time
	FilesScanned   int
	OrphansFound   int
	OrphansDeleted int
	BytesFreed     int64
	Errors         []string
}

// NewOrphanCleanupJob creates a new orphan cleanup job
// Requirements: 4.7 - Run orphan cleanup job daily to remove unreferenced files
func NewOrphanCleanupJob(storage *StorageService, keyChecker StorageKeyChecker, config OrphanCleanupConfig, logger *log.Logger) *OrphanCleanupJob {
	if logger == nil {
		logger = log.Default()
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 1000
	}
	return &OrphanCleanupJob{
		storage:    storage,
		keyChecker: keyChecker,
		config:     config,
		logger:     logger,
		stopChan:   make(chan struct{}),
	}
}

// Start begins the periodic cleanup job
// Requirements: 4.7 - Run orphan cleanup job daily
func (j *OrphanCleanupJob) Start() error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.running {
		return fmt.Errorf("cleanup job is already running")
	}

	if !j.config.Enabled {
		j.logger.Println("Orphan cleanup job is disabled")
		return nil
	}

	j.running = true
	j.stopChan = make(chan struct{})
	j.wg.Add(1)

	go j.run()

	j.logger.Printf("Orphan cleanup job started: interval=%v, age_threshold=%v", j.config.Interval, j.config.AgeThreshold)
	return nil
}

// Stop stops the periodic cleanup job
func (j *OrphanCleanupJob) Stop() {
	j.mu.Lock()
	if !j.running {
		j.mu.Unlock()
		return
	}
	j.running = false
	close(j.stopChan)
	j.mu.Unlock()

	j.wg.Wait()
	j.logger.Println("Orphan cleanup job stopped")
}

// IsRunning returns whether the cleanup job is running
func (j *OrphanCleanupJob) IsRunning() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.running
}

// GetLastResult returns the result of the last cleanup run
func (j *OrphanCleanupJob) GetLastResult() *CleanupResult {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.lastResult
}

// GetLastRunTime returns the time of the last cleanup run
func (j *OrphanCleanupJob) GetLastRunTime() time.Time {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.lastRun
}

// run is the main loop for the cleanup job
func (j *OrphanCleanupJob) run() {
	defer j.wg.Done()

	// Run immediately on start
	j.runCleanup()

	ticker := time.NewTicker(j.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			j.runCleanup()
		case <-j.stopChan:
			return
		}
	}
}

// runCleanup performs a single cleanup run
func (j *OrphanCleanupJob) runCleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	result := &CleanupResult{
		StartTime: time.Now(),
	}

	j.logger.Println("Starting orphan cleanup run...")

	// Find and delete orphaned files
	orphans, scanned, err := j.findOrphans(ctx)
	result.FilesScanned = scanned
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("error finding orphans: %v", err))
		j.logger.Printf("Error finding orphans: %v", err)
	}

	result.OrphansFound = len(orphans)

	if len(orphans) > 0 {
		deleted, bytesFreed, deleteErrors := j.deleteOrphans(ctx, orphans)
		result.OrphansDeleted = deleted
		result.BytesFreed = bytesFreed
		result.Errors = append(result.Errors, deleteErrors...)
	}

	result.EndTime = time.Now()

	j.mu.Lock()
	j.lastRun = result.StartTime
	j.lastResult = result
	j.mu.Unlock()

	j.logger.Printf("Orphan cleanup completed: scanned=%d, found=%d, deleted=%d, bytes_freed=%d, errors=%d, duration=%v",
		result.FilesScanned, result.OrphansFound, result.OrphansDeleted, result.BytesFreed,
		len(result.Errors), result.EndTime.Sub(result.StartTime))
}

// RunNow triggers an immediate cleanup run (for testing or manual trigger)
func (j *OrphanCleanupJob) RunNow(ctx context.Context) (*CleanupResult, error) {
	result := &CleanupResult{
		StartTime: time.Now(),
	}

	// Find orphaned files
	orphans, scanned, err := j.findOrphans(ctx)
	result.FilesScanned = scanned
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("error finding orphans: %v", err))
	}

	result.OrphansFound = len(orphans)

	// Delete orphaned files
	if len(orphans) > 0 {
		deleted, bytesFreed, deleteErrors := j.deleteOrphans(ctx, orphans)
		result.OrphansDeleted = deleted
		result.BytesFreed = bytesFreed
		result.Errors = append(result.Errors, deleteErrors...)
	}

	result.EndTime = time.Now()

	j.mu.Lock()
	j.lastRun = result.StartTime
	j.lastResult = result
	j.mu.Unlock()

	return result, nil
}

// orphanFile represents a file that may be orphaned
type orphanFile struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// findOrphans finds files in storage that don't have corresponding database records
// Requirements: 4.7 - Find files in storage without database records
func (j *OrphanCleanupJob) findOrphans(ctx context.Context) ([]orphanFile, int, error) {
	var orphans []orphanFile
	var scanned int
	cutoffTime := time.Now().Add(-j.config.AgeThreshold)

	// List all objects in the attachments prefix
	prefix := "attachments/"
	paginator := s3.NewListObjectsV2Paginator(j.storage.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(j.storage.bucket),
		Prefix: aws.String(prefix),
	})

	var batch []orphanFile
	for paginator.HasMorePages() {
		select {
		case <-ctx.Done():
			return orphans, scanned, ctx.Err()
		default:
		}

		page, err := paginator.NextPage(ctx)
		if err != nil {
			return orphans, scanned, fmt.Errorf("failed to list objects: %w", err)
		}

		for _, obj := range page.Contents {
			scanned++
			if obj.Key == nil {
				continue
			}

			// Skip files that are too new (within age threshold)
			if obj.LastModified != nil && obj.LastModified.After(cutoffTime) {
				continue
			}

			batch = append(batch, orphanFile{
				Key:          *obj.Key,
				Size:         aws.ToInt64(obj.Size),
				LastModified: aws.ToTime(obj.LastModified),
			})

			// Process batch when it reaches batch size
			if len(batch) >= j.config.BatchSize {
				orphansInBatch, err := j.checkBatchForOrphans(ctx, batch)
				if err != nil {
					j.logger.Printf("Error checking batch for orphans: %v", err)
				} else {
					orphans = append(orphans, orphansInBatch...)
				}
				batch = batch[:0] // Reset batch
			}
		}
	}

	// Process remaining batch
	if len(batch) > 0 {
		orphansInBatch, err := j.checkBatchForOrphans(ctx, batch)
		if err != nil {
			j.logger.Printf("Error checking final batch for orphans: %v", err)
		} else {
			orphans = append(orphans, orphansInBatch...)
		}
	}

	return orphans, scanned, nil
}

// checkBatchForOrphans checks a batch of files against the database
func (j *OrphanCleanupJob) checkBatchForOrphans(ctx context.Context, files []orphanFile) ([]orphanFile, error) {
	if len(files) == 0 {
		return nil, nil
	}

	// Extract keys
	keys := make([]string, len(files))
	for i, f := range files {
		keys[i] = f.Key
	}

	// Check which keys exist in database
	existsMap, err := j.keyChecker.BatchExistsInDatabase(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("failed to check database: %w", err)
	}

	// Find orphans (files not in database)
	var orphans []orphanFile
	for _, f := range files {
		if !existsMap[f.Key] {
			orphans = append(orphans, f)
		}
	}

	return orphans, nil
}

// deleteOrphans deletes orphaned files from storage
// Requirements: 4.7 - Delete orphaned files older than 7 days
func (j *OrphanCleanupJob) deleteOrphans(ctx context.Context, orphans []orphanFile) (int, int64, []string) {
	if len(orphans) == 0 {
		return 0, 0, nil
	}

	var deleted int
	var bytesFreed int64
	var errors []string

	// Delete in batches
	for i := 0; i < len(orphans); i += j.config.BatchSize {
		select {
		case <-ctx.Done():
			errors = append(errors, "context cancelled during deletion")
			return deleted, bytesFreed, errors
		default:
		}

		end := i + j.config.BatchSize
		if end > len(orphans) {
			end = len(orphans)
		}
		batch := orphans[i:end]

		// Build delete objects input
		objectIdentifiers := make([]types.ObjectIdentifier, len(batch))
		var batchSize int64
		for idx, f := range batch {
			objectIdentifiers[idx] = types.ObjectIdentifier{
				Key: aws.String(f.Key),
			}
			batchSize += f.Size
		}

		// Delete batch
		output, err := j.storage.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(j.storage.bucket),
			Delete: &types.Delete{
				Objects: objectIdentifiers,
				Quiet:   aws.Bool(false),
			},
		})
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to delete batch at index %d: %v", i, err))
			continue
		}

		// Count successful deletions
		batchDeleted := len(batch) - len(output.Errors)
		deleted += batchDeleted

		// Calculate bytes freed (approximate based on successful deletions)
		if batchDeleted == len(batch) {
			bytesFreed += batchSize
		} else {
			// Calculate based on which files were actually deleted
			deletedKeys := make(map[string]bool)
			for _, d := range output.Deleted {
				if d.Key != nil {
					deletedKeys[*d.Key] = true
				}
			}
			for _, f := range batch {
				if deletedKeys[f.Key] {
					bytesFreed += f.Size
				}
			}
		}

		// Log any errors
		for _, e := range output.Errors {
			errors = append(errors, fmt.Sprintf("failed to delete %s: %s", aws.ToString(e.Key), aws.ToString(e.Message)))
		}
	}

	return deleted, bytesFreed, errors
}

// GetConfig returns the current configuration
func (j *OrphanCleanupJob) GetConfig() OrphanCleanupConfig {
	return j.config
}

// UpdateConfig updates the configuration (requires restart to take effect)
func (j *OrphanCleanupJob) UpdateConfig(config OrphanCleanupConfig) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.config = config
}

// FormatStorageKey extracts the storage key from a full S3 path
func FormatStorageKey(key string) string {
	// Remove any leading slashes
	return strings.TrimPrefix(key, "/")
}
