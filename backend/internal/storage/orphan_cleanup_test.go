package storage

import (
	"context"
	"testing"
	"time"
)

// MockStorageKeyChecker is a mock implementation of StorageKeyChecker for testing
type MockStorageKeyChecker struct {
	existingKeys map[string]bool
}

func NewMockStorageKeyChecker(existingKeys []string) *MockStorageKeyChecker {
	m := &MockStorageKeyChecker{
		existingKeys: make(map[string]bool),
	}
	for _, key := range existingKeys {
		m.existingKeys[key] = true
	}
	return m
}

func (m *MockStorageKeyChecker) ExistsInDatabase(ctx context.Context, storageKey string) (bool, error) {
	return m.existingKeys[storageKey], nil
}

func (m *MockStorageKeyChecker) BatchExistsInDatabase(ctx context.Context, storageKeys []string) (map[string]bool, error) {
	result := make(map[string]bool, len(storageKeys))
	for _, key := range storageKeys {
		result[key] = m.existingKeys[key]
	}
	return result, nil
}

func TestDefaultOrphanCleanupConfig(t *testing.T) {
	config := DefaultOrphanCleanupConfig()

	if config.Interval != 24*time.Hour {
		t.Errorf("expected interval to be 24 hours, got %v", config.Interval)
	}

	if config.AgeThreshold != 7*24*time.Hour {
		t.Errorf("expected age threshold to be 7 days, got %v", config.AgeThreshold)
	}

	if config.BatchSize != 1000 {
		t.Errorf("expected batch size to be 1000, got %d", config.BatchSize)
	}

	if !config.Enabled {
		t.Error("expected enabled to be true")
	}
}

func TestOrphanCleanupJob_NewWithDefaults(t *testing.T) {
	mockChecker := NewMockStorageKeyChecker(nil)
	config := DefaultOrphanCleanupConfig()

	// Note: We can't create a real StorageService without S3 credentials,
	// so we test the configuration handling
	job := NewOrphanCleanupJob(nil, mockChecker, config, nil)

	if job == nil {
		t.Fatal("expected job to be created")
	}

	if job.config.Interval != config.Interval {
		t.Errorf("expected interval %v, got %v", config.Interval, job.config.Interval)
	}

	if job.config.AgeThreshold != config.AgeThreshold {
		t.Errorf("expected age threshold %v, got %v", config.AgeThreshold, job.config.AgeThreshold)
	}

	if job.config.BatchSize != config.BatchSize {
		t.Errorf("expected batch size %d, got %d", config.BatchSize, job.config.BatchSize)
	}
}

func TestOrphanCleanupJob_DisabledConfig(t *testing.T) {
	mockChecker := NewMockStorageKeyChecker(nil)
	config := OrphanCleanupConfig{
		Enabled: false,
	}

	job := NewOrphanCleanupJob(nil, mockChecker, config, nil)

	// Start should not error when disabled
	err := job.Start()
	if err != nil {
		t.Errorf("expected no error when starting disabled job, got %v", err)
	}

	// Job should not be running
	if job.IsRunning() {
		t.Error("expected job to not be running when disabled")
	}
}

func TestOrphanCleanupJob_BatchSizeDefault(t *testing.T) {
	mockChecker := NewMockStorageKeyChecker(nil)
	config := OrphanCleanupConfig{
		Interval:     time.Hour,
		AgeThreshold: time.Hour,
		BatchSize:    0, // Invalid batch size
		Enabled:      true,
	}

	job := NewOrphanCleanupJob(nil, mockChecker, config, nil)

	// Should default to 1000
	if job.config.BatchSize != 1000 {
		t.Errorf("expected batch size to default to 1000, got %d", job.config.BatchSize)
	}
}

func TestOrphanCleanupJob_UpdateConfig(t *testing.T) {
	mockChecker := NewMockStorageKeyChecker(nil)
	config := DefaultOrphanCleanupConfig()

	job := NewOrphanCleanupJob(nil, mockChecker, config, nil)

	newConfig := OrphanCleanupConfig{
		Interval:     12 * time.Hour,
		AgeThreshold: 3 * 24 * time.Hour,
		BatchSize:    500,
		Enabled:      true,
	}

	job.UpdateConfig(newConfig)

	gotConfig := job.GetConfig()
	if gotConfig.Interval != newConfig.Interval {
		t.Errorf("expected interval %v, got %v", newConfig.Interval, gotConfig.Interval)
	}

	if gotConfig.AgeThreshold != newConfig.AgeThreshold {
		t.Errorf("expected age threshold %v, got %v", newConfig.AgeThreshold, gotConfig.AgeThreshold)
	}

	if gotConfig.BatchSize != newConfig.BatchSize {
		t.Errorf("expected batch size %d, got %d", newConfig.BatchSize, gotConfig.BatchSize)
	}
}

func TestMockStorageKeyChecker_ExistsInDatabase(t *testing.T) {
	existingKeys := []string{"key1", "key2", "key3"}
	checker := NewMockStorageKeyChecker(existingKeys)

	ctx := context.Background()

	// Test existing key
	exists, err := checker.ExistsInDatabase(ctx, "key1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected key1 to exist")
	}

	// Test non-existing key
	exists, err = checker.ExistsInDatabase(ctx, "key4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected key4 to not exist")
	}
}

func TestMockStorageKeyChecker_BatchExistsInDatabase(t *testing.T) {
	existingKeys := []string{"key1", "key2", "key3"}
	checker := NewMockStorageKeyChecker(existingKeys)

	ctx := context.Background()

	keysToCheck := []string{"key1", "key2", "key4", "key5"}
	result, err := checker.BatchExistsInDatabase(ctx, keysToCheck)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result["key1"] {
		t.Error("expected key1 to exist")
	}
	if !result["key2"] {
		t.Error("expected key2 to exist")
	}
	if result["key4"] {
		t.Error("expected key4 to not exist")
	}
	if result["key5"] {
		t.Error("expected key5 to not exist")
	}
}

func TestFormatStorageKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"attachments/user1/email1/file.pdf", "attachments/user1/email1/file.pdf"},
		{"/attachments/user1/email1/file.pdf", "attachments/user1/email1/file.pdf"},
		{"file.pdf", "file.pdf"},
		{"/file.pdf", "file.pdf"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := FormatStorageKey(tt.input)
			if result != tt.expected {
				t.Errorf("FormatStorageKey(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCleanupResult_Fields(t *testing.T) {
	result := &CleanupResult{
		StartTime:      time.Now(),
		EndTime:        time.Now().Add(time.Minute),
		FilesScanned:   100,
		OrphansFound:   10,
		OrphansDeleted: 8,
		BytesFreed:     1024 * 1024,
		Errors:         []string{"error1", "error2"},
	}

	if result.FilesScanned != 100 {
		t.Errorf("expected FilesScanned to be 100, got %d", result.FilesScanned)
	}

	if result.OrphansFound != 10 {
		t.Errorf("expected OrphansFound to be 10, got %d", result.OrphansFound)
	}

	if result.OrphansDeleted != 8 {
		t.Errorf("expected OrphansDeleted to be 8, got %d", result.OrphansDeleted)
	}

	if result.BytesFreed != 1024*1024 {
		t.Errorf("expected BytesFreed to be 1MB, got %d", result.BytesFreed)
	}

	if len(result.Errors) != 2 {
		t.Errorf("expected 2 errors, got %d", len(result.Errors))
	}
}
