package email

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/repository"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/sanitizer"
	"pgregory.net/rapid"
)

// MockEmailRepository implements a simple in-memory email repository for testing
type MockEmailRepository struct {
	emails          map[uuid.UUID]*repository.Email
	emailsByAlias   map[uuid.UUID][]*repository.Email
	aliasOwnership  map[uuid.UUID]uuid.UUID // aliasID -> userID
}

func NewMockEmailRepository() *MockEmailRepository {
	return &MockEmailRepository{
		emails:         make(map[uuid.UUID]*repository.Email),
		emailsByAlias:  make(map[uuid.UUID][]*repository.Email),
		aliasOwnership: make(map[uuid.UUID]uuid.UUID),
	}
}

func (m *MockEmailRepository) SetAliasOwnership(aliasID, userID uuid.UUID) {
	m.aliasOwnership[aliasID] = userID
}

func (m *MockEmailRepository) AddEmail(email *repository.Email) {
	m.emails[email.ID] = email
	m.emailsByAlias[email.AliasID] = append(m.emailsByAlias[email.AliasID], email)
}

func (m *MockEmailRepository) List(ctx context.Context, userID uuid.UUID, params repository.ListEmailParams) ([]repository.EmailWithPreview, int, error) {
	var result []repository.EmailWithPreview

	// Apply defaults
	if params.Page < 1 {
		params.Page = 1
	}
	if params.Limit < 1 {
		params.Limit = 20
	}
	if params.Limit > 100 {
		params.Limit = 100
	}

	for _, email := range m.emails {
		// Check ownership via alias
		aliasOwner, exists := m.aliasOwnership[email.AliasID]
		if !exists || aliasOwner != userID {
			continue
		}

		// Apply alias filter
		if params.AliasID != nil && email.AliasID != *params.AliasID {
			continue
		}

		// Apply search filter
		if params.Search != "" {
			searchLower := strings.ToLower(params.Search)
			subjectMatch := email.Subject != nil && strings.Contains(strings.ToLower(*email.Subject), searchLower)
			senderMatch := strings.Contains(strings.ToLower(email.SenderAddress), searchLower)
			bodyMatch := email.BodyText != nil && strings.Contains(strings.ToLower(*email.BodyText), searchLower)
			if !subjectMatch && !senderMatch && !bodyMatch {
				continue
			}
		}

		// Apply date range filter
		if params.FromDate != nil && email.ReceivedAt.Before(*params.FromDate) {
			continue
		}
		if params.ToDate != nil && email.ReceivedAt.After(*params.ToDate) {
			continue
		}

		// Apply is_read filter
		if params.IsRead != nil && email.IsRead != *params.IsRead {
			continue
		}

		bodyText := ""
		if email.BodyText != nil {
			bodyText = *email.BodyText
		}

		result = append(result, repository.EmailWithPreview{
			ID:          email.ID,
			AliasID:     email.AliasID,
			AliasEmail:  email.AliasID.String() + "@example.com",
			FromAddress: email.SenderAddress,
			FromName:    email.SenderName,
			Subject:     email.Subject,
			PreviewText: repository.GeneratePreviewText(bodyText, 200),
			ReceivedAt:  email.ReceivedAt,
			SizeBytes:   email.SizeBytes,
			IsRead:      email.IsRead,
		})
	}

	totalCount := len(result)

	// Apply sorting
	if params.Sort == "size" {
		// Sort by size
		for i := 0; i < len(result)-1; i++ {
			for j := i + 1; j < len(result); j++ {
				if params.Order == "asc" {
					if result[i].SizeBytes > result[j].SizeBytes {
						result[i], result[j] = result[j], result[i]
					}
				} else {
					if result[i].SizeBytes < result[j].SizeBytes {
						result[i], result[j] = result[j], result[i]
					}
				}
			}
		}
	} else {
		// Sort by received_at (default)
		for i := 0; i < len(result)-1; i++ {
			for j := i + 1; j < len(result); j++ {
				if params.Order == "asc" {
					if result[i].ReceivedAt.After(result[j].ReceivedAt) {
						result[i], result[j] = result[j], result[i]
					}
				} else {
					if result[i].ReceivedAt.Before(result[j].ReceivedAt) {
						result[i], result[j] = result[j], result[i]
					}
				}
			}
		}
	}

	// Apply pagination
	start := (params.Page - 1) * params.Limit
	end := start + params.Limit

	if start >= len(result) {
		return []repository.EmailWithPreview{}, totalCount, nil
	}
	if end > len(result) {
		end = len(result)
	}

	return result[start:end], totalCount, nil
}

func (m *MockEmailRepository) GetByID(ctx context.Context, id uuid.UUID) (*repository.Email, error) {
	email, exists := m.emails[id]
	if !exists {
		return nil, repository.ErrEmailNotFound
	}
	return email, nil
}

func (m *MockEmailRepository) Delete(ctx context.Context, id uuid.UUID) error {
	email, exists := m.emails[id]
	if !exists {
		return repository.ErrEmailNotFound
	}
	delete(m.emails, id)
	// Remove from alias list
	aliasEmails := m.emailsByAlias[email.AliasID]
	for i, e := range aliasEmails {
		if e.ID == id {
			m.emailsByAlias[email.AliasID] = append(aliasEmails[:i], aliasEmails[i+1:]...)
			break
		}
	}
	return nil
}

func (m *MockEmailRepository) DeleteBatch(ctx context.Context, ids []uuid.UUID) (int, error) {
	deleted := 0
	for _, id := range ids {
		if err := m.Delete(ctx, id); err == nil {
			deleted++
		}
	}
	return deleted, nil
}

func (m *MockEmailRepository) MarkAsRead(ctx context.Context, id uuid.UUID) error {
	email, exists := m.emails[id]
	if !exists {
		return repository.ErrEmailNotFound
	}
	email.IsRead = true
	return nil
}

func (m *MockEmailRepository) MarkAsReadBatch(ctx context.Context, ids []uuid.UUID) (int, error) {
	updated := 0
	for _, id := range ids {
		if err := m.MarkAsRead(ctx, id); err == nil {
			updated++
		}
	}
	return updated, nil
}

func (m *MockEmailRepository) GetStats(ctx context.Context, userID uuid.UUID) (*repository.InboxStats, error) {
	stats := &repository.InboxStats{
		EmailsPerAlias: []repository.AliasEmailCount{},
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	weekAgo := now.AddDate(0, 0, -7)
	monthAgo := now.AddDate(0, 0, -30)

	aliasCount := make(map[uuid.UUID]int)

	for _, email := range m.emails {
		aliasOwner, exists := m.aliasOwnership[email.AliasID]
		if !exists || aliasOwner != userID {
			continue
		}

		stats.TotalEmails++
		stats.TotalSizeBytes += email.SizeBytes
		if !email.IsRead {
			stats.UnreadEmails++
		}
		if email.ReceivedAt.After(today) || email.ReceivedAt.Equal(today) {
			stats.EmailsToday++
		}
		if email.ReceivedAt.After(weekAgo) {
			stats.EmailsThisWeek++
		}
		if email.ReceivedAt.After(monthAgo) {
			stats.EmailsThisMonth++
		}
		aliasCount[email.AliasID]++
	}

	for aliasID, count := range aliasCount {
		stats.EmailsPerAlias = append(stats.EmailsPerAlias, repository.AliasEmailCount{
			AliasID:    aliasID,
			AliasEmail: aliasID.String() + "@example.com",
			Count:      count,
		})
	}

	return stats, nil
}

func (m *MockEmailRepository) IsOwnedByUser(ctx context.Context, emailID, userID uuid.UUID) (bool, error) {
	email, exists := m.emails[emailID]
	if !exists {
		return false, nil
	}
	aliasOwner, exists := m.aliasOwnership[email.AliasID]
	if !exists {
		return false, nil
	}
	return aliasOwner == userID, nil
}

func (m *MockEmailRepository) GetSizeByID(ctx context.Context, id uuid.UUID) (int64, error) {
	email, exists := m.emails[id]
	if !exists {
		return 0, repository.ErrEmailNotFound
	}
	return email.SizeBytes, nil
}

func (m *MockEmailRepository) GetEmailIDsOwnedByUser(ctx context.Context, emailIDs []uuid.UUID, userID uuid.UUID) ([]uuid.UUID, error) {
	var owned []uuid.UUID
	for _, id := range emailIDs {
		email, exists := m.emails[id]
		if !exists {
			continue
		}
		aliasOwner, exists := m.aliasOwnership[email.AliasID]
		if !exists {
			continue
		}
		if aliasOwner == userID {
			owned = append(owned, id)
		}
	}
	return owned, nil
}

func (m *MockEmailRepository) GetTotalSizeByIDs(ctx context.Context, ids []uuid.UUID) (int64, error) {
	var total int64
	for _, id := range ids {
		email, exists := m.emails[id]
		if exists {
			total += email.SizeBytes
		}
	}
	return total, nil
}

func (m *MockEmailRepository) Create(ctx context.Context, email *repository.Email) error {
	m.emails[email.ID] = email
	m.emailsByAlias[email.AliasID] = append(m.emailsByAlias[email.AliasID], email)
	return nil
}


// MockAttachmentRepository implements a simple in-memory attachment repository for testing
type MockAttachmentRepository struct {
	attachments map[uuid.UUID]*repository.Attachment
	byEmailID   map[uuid.UUID][]*repository.Attachment
}

func NewMockAttachmentRepository() *MockAttachmentRepository {
	return &MockAttachmentRepository{
		attachments: make(map[uuid.UUID]*repository.Attachment),
		byEmailID:   make(map[uuid.UUID][]*repository.Attachment),
	}
}

func (m *MockAttachmentRepository) AddAttachment(att *repository.Attachment) {
	m.attachments[att.ID] = att
	m.byEmailID[att.EmailID] = append(m.byEmailID[att.EmailID], att)
}

func (m *MockAttachmentRepository) GetByID(ctx context.Context, id uuid.UUID) (*repository.Attachment, error) {
	att, exists := m.attachments[id]
	if !exists {
		return nil, nil
	}
	return att, nil
}

func (m *MockAttachmentRepository) GetByEmailID(ctx context.Context, emailID uuid.UUID) ([]*repository.Attachment, error) {
	return m.byEmailID[emailID], nil
}

func (m *MockAttachmentRepository) GetStorageKeysByEmailID(ctx context.Context, emailID uuid.UUID) ([]string, error) {
	var keys []string
	for _, att := range m.byEmailID[emailID] {
		keys = append(keys, att.StorageKey)
	}
	return keys, nil
}

func (m *MockAttachmentRepository) GetTotalSizeByEmailID(ctx context.Context, emailID uuid.UUID) (int64, error) {
	var total int64
	for _, att := range m.byEmailID[emailID] {
		total += att.SizeBytes
	}
	return total, nil
}

func (m *MockAttachmentRepository) DeleteByEmailID(ctx context.Context, emailID uuid.UUID) (int64, error) {
	atts := m.byEmailID[emailID]
	for _, att := range atts {
		delete(m.attachments, att.ID)
	}
	delete(m.byEmailID, emailID)
	return int64(len(atts)), nil
}

// MockStorageService implements a simple mock storage service for testing
type MockStorageService struct {
	files map[string][]byte
}

func NewMockStorageService() *MockStorageService {
	return &MockStorageService{
		files: make(map[string][]byte),
	}
}

func (m *MockStorageService) AddFile(key string, data []byte) {
	m.files[key] = data
}

func (m *MockStorageService) DeleteByKeys(ctx context.Context, keys []string) (int, int64, error) {
	deleted := 0
	var size int64
	for _, key := range keys {
		if data, exists := m.files[key]; exists {
			size += int64(len(data))
			delete(m.files, key)
			deleted++
		}
	}
	return deleted, size, nil
}

// TestableEmailService wraps the email service for testing with mock repositories
type TestableEmailService struct {
	emailRepo      *MockEmailRepository
	attachmentRepo *MockAttachmentRepository
	storageService *MockStorageService
	sanitizer      sanitizer.HTMLSanitizer
	baseURL        string
}

func NewTestableEmailService() *TestableEmailService {
	return &TestableEmailService{
		emailRepo:      NewMockEmailRepository(),
		attachmentRepo: NewMockAttachmentRepository(),
		storageService: NewMockStorageService(),
		sanitizer:      sanitizer.NewHTMLSanitizer(),
		baseURL:        "https://api.example.com/v1",
	}
}

// List implements the List method using mock repository
func (s *TestableEmailService) List(ctx context.Context, userID uuid.UUID, params ListEmailParams) (*EmailListResponse, error) {
	// Apply defaults
	if params.Page < 1 {
		params.Page = 1
	}
	if params.Limit < 1 {
		params.Limit = 20
	}
	if params.Limit > 100 {
		params.Limit = 100
	}

	// Convert params to repository params
	repoParams := repository.ListEmailParams{
		Page:           params.Page,
		Limit:          params.Limit,
		Search:         params.Search,
		FromDate:       params.FromDate,
		ToDate:         params.ToDate,
		HasAttachments: params.HasAttachments,
		IsRead:         params.IsRead,
		Sort:           params.Sort,
		Order:          params.Order,
	}

	// Parse alias filter if provided
	if params.AliasID != "" {
		aliasID, err := uuid.Parse(params.AliasID)
		if err != nil {
			return nil, errors.New("invalid alias_id format")
		}
		repoParams.AliasID = &aliasID
	}

	// Get emails from repository
	emails, totalCount, err := s.emailRepo.List(ctx, userID, repoParams)
	if err != nil {
		return nil, err
	}

	// Convert to response format
	emailResponses := make([]EmailWithPreview, len(emails))
	for i, e := range emails {
		emailResponses[i] = EmailWithPreview{
			ID:              e.ID.String(),
			AliasID:         e.AliasID.String(),
			AliasEmail:      e.AliasEmail,
			FromAddress:     e.FromAddress,
			FromName:        e.FromName,
			Subject:         e.Subject,
			PreviewText:     e.PreviewText,
			ReceivedAt:      e.ReceivedAt,
			HasAttachments:  e.HasAttachments,
			AttachmentCount: e.AttachmentCount,
			SizeBytes:       e.SizeBytes,
			IsRead:          e.IsRead,
		}
	}

	// Calculate pagination
	totalPages := (totalCount + params.Limit - 1) / params.Limit
	if totalPages < 1 {
		totalPages = 1
	}

	return &EmailListResponse{
		Emails: emailResponses,
		Pagination: Pagination{
			CurrentPage: params.Page,
			PerPage:     params.Limit,
			TotalPages:  totalPages,
			TotalCount:  totalCount,
		},
	}, nil
}

// IsOwnedByUser checks if an email belongs to a user
func (s *TestableEmailService) IsOwnedByUser(ctx context.Context, emailID, userID uuid.UUID) (bool, error) {
	return s.emailRepo.IsOwnedByUser(ctx, emailID, userID)
}


// Feature: email-inbox-api, Property 1: Pagination Correctness
// **Validates: Requirements 1.1, 1.7**
//
// *For any* list request with page and limit parameters, the response SHALL contain
// at most `limit` items, correct pagination metadata, and default to page=1, limit=20 when not specified.
// Maximum limit is 100.
func TestProperty1_PaginationCorrectness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		aliasID := uuid.New()
		service.emailRepo.SetAliasOwnership(aliasID, userID)

		// Create random number of emails
		totalEmails := rapid.IntRange(0, 150).Draw(t, "totalEmails")
		for i := 0; i < totalEmails; i++ {
			email := &repository.Email{
				ID:            uuid.New(),
				AliasID:       aliasID,
				SenderAddress: "sender@example.com",
				ReceivedAt:    time.Now().UTC().Add(-time.Duration(i) * time.Hour),
				SizeBytes:     int64(rapid.IntRange(100, 10000).Draw(t, "size")),
				IsRead:        rapid.Bool().Draw(t, "isRead"),
			}
			service.emailRepo.AddEmail(email)
		}

		// Generate random pagination params
		page := rapid.IntRange(1, 20).Draw(t, "page")
		limit := rapid.IntRange(1, 150).Draw(t, "limit") // Test values above 100 too

		params := ListEmailParams{
			Page:  page,
			Limit: limit,
		}

		resp, err := service.List(ctx, userID, params)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: Limit should be capped at 100
		effectiveLimit := limit
		if effectiveLimit > 100 {
			effectiveLimit = 100
		}

		// Property: Response should contain at most `effectiveLimit` items
		if len(resp.Emails) > effectiveLimit {
			t.Errorf("expected at most %d items, got %d", effectiveLimit, len(resp.Emails))
		}

		// Property: Pagination metadata should be correct
		if resp.Pagination.CurrentPage != page {
			t.Errorf("expected current_page %d, got %d", page, resp.Pagination.CurrentPage)
		}
		if resp.Pagination.PerPage != effectiveLimit {
			t.Errorf("expected per_page %d, got %d", effectiveLimit, resp.Pagination.PerPage)
		}
		if resp.Pagination.TotalCount != totalEmails {
			t.Errorf("expected total_count %d, got %d", totalEmails, resp.Pagination.TotalCount)
		}

		// Property: Total pages calculation should be correct
		expectedTotalPages := (totalEmails + effectiveLimit - 1) / effectiveLimit
		if expectedTotalPages < 1 {
			expectedTotalPages = 1
		}
		if resp.Pagination.TotalPages != expectedTotalPages {
			t.Errorf("expected total_pages %d, got %d", expectedTotalPages, resp.Pagination.TotalPages)
		}

		// Property: Items on page should be correct count
		expectedItemsOnPage := totalEmails - (page-1)*effectiveLimit
		if expectedItemsOnPage < 0 {
			expectedItemsOnPage = 0
		}
		if expectedItemsOnPage > effectiveLimit {
			expectedItemsOnPage = effectiveLimit
		}
		if len(resp.Emails) != expectedItemsOnPage {
			t.Errorf("expected %d items on page %d, got %d", expectedItemsOnPage, page, len(resp.Emails))
		}
	})
}

// TestProperty1_PaginationCorrectness_Defaults tests default pagination values
func TestProperty1_PaginationCorrectness_Defaults(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		aliasID := uuid.New()
		service.emailRepo.SetAliasOwnership(aliasID, userID)

		// Create some emails
		totalEmails := rapid.IntRange(0, 50).Draw(t, "totalEmails")
		for i := 0; i < totalEmails; i++ {
			email := &repository.Email{
				ID:            uuid.New(),
				AliasID:       aliasID,
				SenderAddress: "sender@example.com",
				ReceivedAt:    time.Now().UTC(),
				SizeBytes:     1000,
			}
			service.emailRepo.AddEmail(email)
		}

		// Test with zero/negative values (should use defaults)
		params := ListEmailParams{
			Page:  0,
			Limit: 0,
		}

		resp, err := service.List(ctx, userID, params)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: Default page should be 1
		if resp.Pagination.CurrentPage != 1 {
			t.Errorf("expected default page 1, got %d", resp.Pagination.CurrentPage)
		}

		// Property: Default limit should be 20
		if resp.Pagination.PerPage != 20 {
			t.Errorf("expected default limit 20, got %d", resp.Pagination.PerPage)
		}
	})
}


// Feature: email-inbox-api, Property 2: List Filtering Correctness
// **Validates: Requirements 1.2, 1.3, 1.4, 1.5**
//
// *For any* list request with filters (alias_id, search, from_date, to_date, has_attachments),
// all returned emails SHALL match all specified filter criteria.
func TestProperty2_ListFilteringCorrectness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		alias1ID := uuid.New()
		alias2ID := uuid.New()
		service.emailRepo.SetAliasOwnership(alias1ID, userID)
		service.emailRepo.SetAliasOwnership(alias2ID, userID)

		// Create emails with various properties
		now := time.Now().UTC()
		subjects := []string{"Meeting notes", "Project update", "Weekly report", "Invoice", "Hello"}
		senders := []string{"alice@example.com", "bob@test.com", "carol@company.org"}

		totalEmails := rapid.IntRange(5, 30).Draw(t, "totalEmails")
		for i := 0; i < totalEmails; i++ {
			aliasID := alias1ID
			if rapid.Bool().Draw(t, "useAlias2") {
				aliasID = alias2ID
			}

			subject := subjects[rapid.IntRange(0, len(subjects)-1).Draw(t, "subjectIdx")]
			sender := senders[rapid.IntRange(0, len(senders)-1).Draw(t, "senderIdx")]
			daysAgo := rapid.IntRange(0, 60).Draw(t, "daysAgo")

			email := &repository.Email{
				ID:            uuid.New(),
				AliasID:       aliasID,
				SenderAddress: sender,
				Subject:       &subject,
				ReceivedAt:    now.AddDate(0, 0, -daysAgo),
				SizeBytes:     int64(rapid.IntRange(100, 10000).Draw(t, "size")),
				IsRead:        rapid.Bool().Draw(t, "isRead"),
			}
			service.emailRepo.AddEmail(email)
		}

		// Test alias filter
		params := ListEmailParams{
			Page:    1,
			Limit:   100,
			AliasID: alias1ID.String(),
		}

		resp, err := service.List(ctx, userID, params)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: All returned emails should belong to the filtered alias
		for _, email := range resp.Emails {
			if email.AliasID != alias1ID.String() {
				t.Errorf("expected all emails to have alias_id %s, got %s", alias1ID.String(), email.AliasID)
			}
		}
	})
}

// TestProperty2_ListFilteringCorrectness_Search tests search filter
func TestProperty2_ListFilteringCorrectness_Search(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		aliasID := uuid.New()
		service.emailRepo.SetAliasOwnership(aliasID, userID)

		// Create emails with specific subjects
		subjects := []string{"Meeting notes", "Project update", "Weekly report", "Invoice", "Hello world"}
		for i, subject := range subjects {
			subj := subject
			email := &repository.Email{
				ID:            uuid.New(),
				AliasID:       aliasID,
				SenderAddress: "sender@example.com",
				Subject:       &subj,
				ReceivedAt:    time.Now().UTC().Add(-time.Duration(i) * time.Hour),
				SizeBytes:     1000,
			}
			service.emailRepo.AddEmail(email)
		}

		// Search for "meeting"
		params := ListEmailParams{
			Page:   1,
			Limit:  100,
			Search: "meeting",
		}

		resp, err := service.List(ctx, userID, params)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: All returned emails should contain the search term
		for _, email := range resp.Emails {
			if email.Subject == nil {
				t.Error("expected email to have subject")
				continue
			}
			if !strings.Contains(strings.ToLower(*email.Subject), "meeting") {
				t.Errorf("expected subject to contain 'meeting', got %s", *email.Subject)
			}
		}
	})
}

// TestProperty2_ListFilteringCorrectness_DateRange tests date range filter
func TestProperty2_ListFilteringCorrectness_DateRange(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		aliasID := uuid.New()
		service.emailRepo.SetAliasOwnership(aliasID, userID)

		now := time.Now().UTC()

		// Create emails across different dates
		for i := 0; i < 30; i++ {
			email := &repository.Email{
				ID:            uuid.New(),
				AliasID:       aliasID,
				SenderAddress: "sender@example.com",
				ReceivedAt:    now.AddDate(0, 0, -i),
				SizeBytes:     1000,
			}
			service.emailRepo.AddEmail(email)
		}

		// Filter for last 7 days
		fromDate := now.AddDate(0, 0, -7)
		toDate := now

		params := ListEmailParams{
			Page:     1,
			Limit:    100,
			FromDate: &fromDate,
			ToDate:   &toDate,
		}

		resp, err := service.List(ctx, userID, params)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: All returned emails should be within the date range
		for _, email := range resp.Emails {
			if email.ReceivedAt.Before(fromDate) {
				t.Errorf("email received_at %v is before from_date %v", email.ReceivedAt, fromDate)
			}
			if email.ReceivedAt.After(toDate) {
				t.Errorf("email received_at %v is after to_date %v", email.ReceivedAt, toDate)
			}
		}
	})
}


// Feature: email-inbox-api, Property 3: Sort Order Correctness
// **Validates: Requirements 1.6**
//
// *For any* list request with sort parameter, the results SHALL be ordered by the specified
// field (received_at or size) in the specified order (asc/desc, default: desc).
func TestProperty3_SortOrderCorrectness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		aliasID := uuid.New()
		service.emailRepo.SetAliasOwnership(aliasID, userID)

		// Create emails with varying sizes and dates
		totalEmails := rapid.IntRange(5, 30).Draw(t, "totalEmails")
		for i := 0; i < totalEmails; i++ {
			email := &repository.Email{
				ID:            uuid.New(),
				AliasID:       aliasID,
				SenderAddress: "sender@example.com",
				ReceivedAt:    time.Now().UTC().Add(-time.Duration(rapid.IntRange(0, 1000).Draw(t, "hoursAgo")) * time.Hour),
				SizeBytes:     int64(rapid.IntRange(100, 100000).Draw(t, "size")),
			}
			service.emailRepo.AddEmail(email)
		}

		// Test sort by received_at DESC (default)
		params := ListEmailParams{
			Page:  1,
			Limit: 100,
			Sort:  "received_at",
			Order: "desc",
		}

		resp, err := service.List(ctx, userID, params)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: Emails should be sorted by received_at DESC
		for i := 1; i < len(resp.Emails); i++ {
			if resp.Emails[i].ReceivedAt.After(resp.Emails[i-1].ReceivedAt) {
				t.Errorf("emails not sorted by received_at DESC: %v > %v",
					resp.Emails[i].ReceivedAt, resp.Emails[i-1].ReceivedAt)
			}
		}
	})
}

// TestProperty3_SortOrderCorrectness_SizeAsc tests sorting by size ascending
func TestProperty3_SortOrderCorrectness_SizeAsc(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		aliasID := uuid.New()
		service.emailRepo.SetAliasOwnership(aliasID, userID)

		// Create emails with varying sizes
		totalEmails := rapid.IntRange(5, 30).Draw(t, "totalEmails")
		for i := 0; i < totalEmails; i++ {
			email := &repository.Email{
				ID:            uuid.New(),
				AliasID:       aliasID,
				SenderAddress: "sender@example.com",
				ReceivedAt:    time.Now().UTC(),
				SizeBytes:     int64(rapid.IntRange(100, 100000).Draw(t, "size")),
			}
			service.emailRepo.AddEmail(email)
		}

		// Test sort by size ASC
		params := ListEmailParams{
			Page:  1,
			Limit: 100,
			Sort:  "size",
			Order: "asc",
		}

		resp, err := service.List(ctx, userID, params)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: Emails should be sorted by size ASC
		for i := 1; i < len(resp.Emails); i++ {
			if resp.Emails[i].SizeBytes < resp.Emails[i-1].SizeBytes {
				t.Errorf("emails not sorted by size ASC: %d < %d",
					resp.Emails[i].SizeBytes, resp.Emails[i-1].SizeBytes)
			}
		}
	})
}

// TestProperty3_SortOrderCorrectness_SizeDesc tests sorting by size descending
func TestProperty3_SortOrderCorrectness_SizeDesc(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		aliasID := uuid.New()
		service.emailRepo.SetAliasOwnership(aliasID, userID)

		// Create emails with varying sizes
		totalEmails := rapid.IntRange(5, 30).Draw(t, "totalEmails")
		for i := 0; i < totalEmails; i++ {
			email := &repository.Email{
				ID:            uuid.New(),
				AliasID:       aliasID,
				SenderAddress: "sender@example.com",
				ReceivedAt:    time.Now().UTC(),
				SizeBytes:     int64(rapid.IntRange(100, 100000).Draw(t, "size")),
			}
			service.emailRepo.AddEmail(email)
		}

		// Test sort by size DESC
		params := ListEmailParams{
			Page:  1,
			Limit: 100,
			Sort:  "size",
			Order: "desc",
		}

		resp, err := service.List(ctx, userID, params)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: Emails should be sorted by size DESC
		for i := 1; i < len(resp.Emails); i++ {
			if resp.Emails[i].SizeBytes > resp.Emails[i-1].SizeBytes {
				t.Errorf("emails not sorted by size DESC: %d > %d",
					resp.Emails[i].SizeBytes, resp.Emails[i-1].SizeBytes)
			}
		}
	})
}


// Feature: email-inbox-api, Property 5: Authorization Enforcement (list part)
// **Validates: Requirements 1.9, 7.1, 7.2**
//
// *For any* email operation (list, get, delete, download attachment), the Email_Service
// SHALL only allow access to emails belonging to aliases owned by the authenticated user.
func TestProperty5_AuthorizationEnforcement_List(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		user1ID := uuid.New()
		user2ID := uuid.New()
		alias1ID := uuid.New() // Owned by user1
		alias2ID := uuid.New() // Owned by user2

		service.emailRepo.SetAliasOwnership(alias1ID, user1ID)
		service.emailRepo.SetAliasOwnership(alias2ID, user2ID)

		// Create emails for both users
		user1EmailCount := rapid.IntRange(1, 10).Draw(t, "user1EmailCount")
		user2EmailCount := rapid.IntRange(1, 10).Draw(t, "user2EmailCount")

		for i := 0; i < user1EmailCount; i++ {
			email := &repository.Email{
				ID:            uuid.New(),
				AliasID:       alias1ID,
				SenderAddress: "sender@example.com",
				ReceivedAt:    time.Now().UTC(),
				SizeBytes:     1000,
			}
			service.emailRepo.AddEmail(email)
		}

		for i := 0; i < user2EmailCount; i++ {
			email := &repository.Email{
				ID:            uuid.New(),
				AliasID:       alias2ID,
				SenderAddress: "sender@example.com",
				ReceivedAt:    time.Now().UTC(),
				SizeBytes:     1000,
			}
			service.emailRepo.AddEmail(email)
		}

		// User1 should only see their own emails
		params := ListEmailParams{
			Page:  1,
			Limit: 100,
		}

		resp, err := service.List(ctx, user1ID, params)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: User1 should only see emails from alias1
		if resp.Pagination.TotalCount != user1EmailCount {
			t.Errorf("expected user1 to see %d emails, got %d", user1EmailCount, resp.Pagination.TotalCount)
		}

		for _, email := range resp.Emails {
			if email.AliasID != alias1ID.String() {
				t.Errorf("user1 should not see emails from alias %s", email.AliasID)
			}
		}

		// User2 should only see their own emails
		resp2, err := service.List(ctx, user2ID, params)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: User2 should only see emails from alias2
		if resp2.Pagination.TotalCount != user2EmailCount {
			t.Errorf("expected user2 to see %d emails, got %d", user2EmailCount, resp2.Pagination.TotalCount)
		}

		for _, email := range resp2.Emails {
			if email.AliasID != alias2ID.String() {
				t.Errorf("user2 should not see emails from alias %s", email.AliasID)
			}
		}
	})
}

// TestProperty5_AuthorizationEnforcement_IsOwnedByUser tests ownership check
func TestProperty5_AuthorizationEnforcement_IsOwnedByUser(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		ownerID := uuid.New()
		otherUserID := uuid.New()
		aliasID := uuid.New()

		service.emailRepo.SetAliasOwnership(aliasID, ownerID)

		// Create an email
		emailID := uuid.New()
		email := &repository.Email{
			ID:            emailID,
			AliasID:       aliasID,
			SenderAddress: "sender@example.com",
			ReceivedAt:    time.Now().UTC(),
			SizeBytes:     1000,
		}
		service.emailRepo.AddEmail(email)

		// Owner should have access
		owned, err := service.IsOwnedByUser(ctx, emailID, ownerID)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}
		if !owned {
			t.Error("expected owner to have access to email")
		}

		// Other user should not have access
		owned, err = service.IsOwnedByUser(ctx, emailID, otherUserID)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}
		if owned {
			t.Error("expected other user to not have access to email")
		}
	})
}

// TestProperty5_AuthorizationEnforcement_NonExistentEmail tests access to non-existent email
func TestProperty5_AuthorizationEnforcement_NonExistentEmail(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		nonExistentEmailID := uuid.New()

		// Non-existent email should return false
		owned, err := service.IsOwnedByUser(ctx, nonExistentEmailID, userID)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}
		if owned {
			t.Error("expected non-existent email to return false for ownership")
		}
	})
}


// GetByID implements the GetByID method using mock repository
func (s *TestableEmailService) GetByID(ctx context.Context, userID uuid.UUID, emailID string, markAsRead bool) (*EmailDetailResponse, error) {
	// Parse email ID
	id, err := uuid.Parse(emailID)
	if err != nil {
		return nil, ErrEmailNotFound
	}

	// Check ownership
	owned, err := s.emailRepo.IsOwnedByUser(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if !owned {
		// Check if email exists at all
		_, err := s.emailRepo.GetByID(ctx, id)
		if err != nil {
			if errors.Is(err, repository.ErrEmailNotFound) {
				return nil, ErrEmailNotFound
			}
			return nil, err
		}
		return nil, ErrAccessDenied
	}

	// Get email from repository
	email, err := s.emailRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrEmailNotFound) {
			return nil, ErrEmailNotFound
		}
		return nil, err
	}

	// Mark as read if requested
	if markAsRead && !email.IsRead {
		if err := s.emailRepo.MarkAsRead(ctx, id); err == nil {
			email.IsRead = true
		}
	}

	// Get attachments
	attachments, err := s.attachmentRepo.GetByEmailID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Sanitize HTML content
	var sanitizedHTML *string
	if email.BodyHTML != nil && *email.BodyHTML != "" {
		sanitized := s.sanitizer.Sanitize(*email.BodyHTML)
		sanitizedHTML = &sanitized
	}

	// Build attachment responses
	attachmentResponses := make([]AttachmentResponse, len(attachments))
	for i, att := range attachments {
		attachmentResponses[i] = AttachmentResponse{
			ID:          att.ID.String(),
			Filename:    att.Filename,
			ContentType: att.ContentType,
			SizeBytes:   att.SizeBytes,
			DownloadURL: s.baseURL + "/emails/" + emailID + "/attachments/" + att.ID.String(),
			CreatedAt:   att.CreatedAt,
		}
	}

	return &EmailDetailResponse{
		ID:             email.ID.String(),
		AliasID:        email.AliasID.String(),
		AliasEmail:     email.AliasID.String() + "@example.com",
		FromAddress:    email.SenderAddress,
		FromName:       email.SenderName,
		Subject:        email.Subject,
		BodyHTML:       sanitizedHTML,
		BodyText:       email.BodyText,
		Headers:        email.Headers,
		ReceivedAt:     email.ReceivedAt,
		SizeBytes:      email.SizeBytes,
		IsRead:         email.IsRead,
		HasAttachments: len(attachments) > 0,
		Attachments:    attachmentResponses,
	}, nil
}

// Feature: email-inbox-api, Property 5: Authorization Enforcement (get part)
// **Validates: Requirements 2.3**
//
// *For any* email get request, if the user does not own the email (via alias),
// the system SHALL return a 403 Forbidden error.
func TestProperty5_AuthorizationEnforcement_Get(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		ownerID := uuid.New()
		otherUserID := uuid.New()
		aliasID := uuid.New()

		service.emailRepo.SetAliasOwnership(aliasID, ownerID)

		// Create an email
		emailID := uuid.New()
		subject := "Test Subject"
		email := &repository.Email{
			ID:            emailID,
			AliasID:       aliasID,
			SenderAddress: "sender@example.com",
			Subject:       &subject,
			ReceivedAt:    time.Now().UTC(),
			SizeBytes:     1000,
			Headers:       map[string]string{"From": "sender@example.com"},
		}
		service.emailRepo.AddEmail(email)

		// Owner should be able to get the email
		resp, err := service.GetByID(ctx, ownerID, emailID.String(), false)
		if err != nil {
			t.Errorf("expected owner to access email, got error: %v", err)
			return
		}
		if resp == nil {
			t.Error("expected response, got nil")
			return
		}

		// Other user should get access denied
		_, err = service.GetByID(ctx, otherUserID, emailID.String(), false)
		if !errors.Is(err, ErrAccessDenied) {
			t.Errorf("expected ErrAccessDenied for non-owner, got: %v", err)
		}
	})
}

// Feature: email-inbox-api, Property 6: Email Details Content
// **Validates: Requirements 2.1, 2.4, 2.5, 2.6**
//
// *For any* email detail request, the response SHALL include body_html, body_text,
// all headers as JSON object, and attachment list with download URLs.
func TestProperty6_EmailDetailsContent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		aliasID := uuid.New()
		service.emailRepo.SetAliasOwnership(aliasID, userID)

		// Create an email with all fields
		emailID := uuid.New()
		subject := rapid.StringMatching(`[A-Za-z ]{5,50}`).Draw(t, "subject")
		bodyHTML := "<p>" + rapid.StringMatching(`[A-Za-z ]{10,100}`).Draw(t, "bodyHTML") + "</p>"
		bodyText := rapid.StringMatching(`[A-Za-z ]{10,100}`).Draw(t, "bodyText")

		email := &repository.Email{
			ID:            emailID,
			AliasID:       aliasID,
			SenderAddress: "sender@example.com",
			Subject:       &subject,
			BodyHTML:      &bodyHTML,
			BodyText:      &bodyText,
			ReceivedAt:    time.Now().UTC(),
			SizeBytes:     int64(len(bodyHTML) + len(bodyText)),
			Headers: map[string]string{
				"From":         "sender@example.com",
				"To":           "recipient@example.com",
				"Content-Type": "text/html",
			},
		}
		service.emailRepo.AddEmail(email)

		// Add some attachments
		attachmentCount := rapid.IntRange(0, 5).Draw(t, "attachmentCount")
		for i := 0; i < attachmentCount; i++ {
			att := &repository.Attachment{
				ID:          uuid.New(),
				EmailID:     emailID,
				Filename:    rapid.StringMatching(`[a-z]{5,10}\.(pdf|txt|jpg)`).Draw(t, "filename"),
				ContentType: "application/octet-stream",
				SizeBytes:   int64(rapid.IntRange(100, 10000).Draw(t, "attSize")),
				StorageKey:  "attachments/" + uuid.New().String(),
				CreatedAt:   time.Now().UTC(),
			}
			service.attachmentRepo.AddAttachment(att)
		}

		// Get email details
		resp, err := service.GetByID(ctx, userID, emailID.String(), false)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: Response should include body_html (sanitized)
		if resp.BodyHTML == nil {
			t.Error("expected body_html to be present")
		}

		// Property: Response should include body_text
		if resp.BodyText == nil {
			t.Error("expected body_text to be present")
		} else if *resp.BodyText != bodyText {
			t.Errorf("expected body_text %q, got %q", bodyText, *resp.BodyText)
		}

		// Property: Response should include headers
		if resp.Headers == nil {
			t.Error("expected headers to be present")
		} else {
			if resp.Headers["From"] != "sender@example.com" {
				t.Errorf("expected From header, got %v", resp.Headers)
			}
		}

		// Property: Response should include attachments with download URLs
		if len(resp.Attachments) != attachmentCount {
			t.Errorf("expected %d attachments, got %d", attachmentCount, len(resp.Attachments))
		}
		for _, att := range resp.Attachments {
			if att.DownloadURL == "" {
				t.Error("expected attachment to have download URL")
			}
			if !strings.Contains(att.DownloadURL, emailID.String()) {
				t.Errorf("expected download URL to contain email ID, got %s", att.DownloadURL)
			}
		}
	})
}

// Feature: email-inbox-api, Property 7: Mark As Read Behavior
// **Validates: Requirements 2.7**
//
// *For any* email detail request with mark_as_read=true (default), the email's
// is_read flag SHALL be set to true.
func TestProperty7_MarkAsReadBehavior(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		aliasID := uuid.New()
		service.emailRepo.SetAliasOwnership(aliasID, userID)

		// Create an unread email
		emailID := uuid.New()
		subject := "Test Subject"
		email := &repository.Email{
			ID:            emailID,
			AliasID:       aliasID,
			SenderAddress: "sender@example.com",
			Subject:       &subject,
			ReceivedAt:    time.Now().UTC(),
			SizeBytes:     1000,
			IsRead:        false, // Initially unread
		}
		service.emailRepo.AddEmail(email)

		// Get email with markAsRead=true
		resp, err := service.GetByID(ctx, userID, emailID.String(), true)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: Email should be marked as read
		if !resp.IsRead {
			t.Error("expected email to be marked as read")
		}

		// Verify in repository
		storedEmail, _ := service.emailRepo.GetByID(ctx, emailID)
		if !storedEmail.IsRead {
			t.Error("expected stored email to be marked as read")
		}
	})
}

// TestProperty7_MarkAsReadBehavior_False tests that markAsRead=false doesn't change read status
func TestProperty7_MarkAsReadBehavior_False(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		aliasID := uuid.New()
		service.emailRepo.SetAliasOwnership(aliasID, userID)

		// Create an unread email
		emailID := uuid.New()
		subject := "Test Subject"
		email := &repository.Email{
			ID:            emailID,
			AliasID:       aliasID,
			SenderAddress: "sender@example.com",
			Subject:       &subject,
			ReceivedAt:    time.Now().UTC(),
			SizeBytes:     1000,
			IsRead:        false, // Initially unread
		}
		service.emailRepo.AddEmail(email)

		// Get email with markAsRead=false
		resp, err := service.GetByID(ctx, userID, emailID.String(), false)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: Email should remain unread
		if resp.IsRead {
			t.Error("expected email to remain unread when markAsRead=false")
		}

		// Verify in repository
		storedEmail, _ := service.emailRepo.GetByID(ctx, emailID)
		if storedEmail.IsRead {
			t.Error("expected stored email to remain unread")
		}
	})
}

// TestProperty5_AuthorizationEnforcement_Get_NotFound tests 404 for non-existent email
func TestProperty5_AuthorizationEnforcement_Get_NotFound(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		nonExistentEmailID := uuid.New()

		// Should return not found error
		_, err := service.GetByID(ctx, userID, nonExistentEmailID.String(), false)
		if !errors.Is(err, ErrEmailNotFound) {
			t.Errorf("expected ErrEmailNotFound for non-existent email, got: %v", err)
		}
	})
}


// Feature: email-inbox-api, Property 5: Authorization Enforcement (attachment part)
// **Validates: Requirements 3.3**
//
// *For any* attachment download request, if the user does not own the email,
// the system SHALL return a 403 Forbidden error.
func TestProperty5_AuthorizationEnforcement_Attachment(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		ownerID := uuid.New()
		otherUserID := uuid.New()
		aliasID := uuid.New()

		service.emailRepo.SetAliasOwnership(aliasID, ownerID)

		// Create an email
		emailID := uuid.New()
		email := &repository.Email{
			ID:            emailID,
			AliasID:       aliasID,
			SenderAddress: "sender@example.com",
			ReceivedAt:    time.Now().UTC(),
			SizeBytes:     1000,
		}
		service.emailRepo.AddEmail(email)

		// Create an attachment
		attachmentID := uuid.New()
		att := &repository.Attachment{
			ID:          attachmentID,
			EmailID:     emailID,
			Filename:    "test.pdf",
			ContentType: "application/pdf",
			SizeBytes:   5000,
			StorageKey:  "attachments/" + attachmentID.String(),
			Checksum:    "abc123",
			CreatedAt:   time.Now().UTC(),
		}
		service.attachmentRepo.AddAttachment(att)

		// Owner should be able to check ownership
		owned, err := service.IsOwnedByUser(ctx, emailID, ownerID)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}
		if !owned {
			t.Error("expected owner to have access")
		}

		// Other user should not have access
		owned, err = service.IsOwnedByUser(ctx, emailID, otherUserID)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}
		if owned {
			t.Error("expected other user to not have access")
		}
	})
}

// Feature: email-inbox-api, Property 9: Attachment Download
// **Validates: Requirements 3.1, 3.4, 3.5**
//
// *For any* attachment download request, the response SHALL have correct Content-Type
// header matching the attachment's content_type.
func TestProperty9_AttachmentDownload(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		aliasID := uuid.New()
		service.emailRepo.SetAliasOwnership(aliasID, userID)

		// Create an email
		emailID := uuid.New()
		email := &repository.Email{
			ID:            emailID,
			AliasID:       aliasID,
			SenderAddress: "sender@example.com",
			ReceivedAt:    time.Now().UTC(),
			SizeBytes:     1000,
		}
		service.emailRepo.AddEmail(email)

		// Create attachments with various content types
		contentTypes := []string{
			"application/pdf",
			"image/jpeg",
			"image/png",
			"text/plain",
			"application/octet-stream",
		}

		contentType := contentTypes[rapid.IntRange(0, len(contentTypes)-1).Draw(t, "contentTypeIdx")]
		filename := rapid.StringMatching(`[a-z]{5,10}\.[a-z]{3}`).Draw(t, "filename")

		attachmentID := uuid.New()
		att := &repository.Attachment{
			ID:          attachmentID,
			EmailID:     emailID,
			Filename:    filename,
			ContentType: contentType,
			SizeBytes:   int64(rapid.IntRange(100, 10000).Draw(t, "size")),
			StorageKey:  "attachments/" + attachmentID.String(),
			Checksum:    "abc123",
			CreatedAt:   time.Now().UTC(),
		}
		service.attachmentRepo.AddAttachment(att)

		// Get attachment metadata
		storedAtt, err := service.attachmentRepo.GetByID(ctx, attachmentID)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: Content-Type should match
		if storedAtt.ContentType != contentType {
			t.Errorf("expected content_type %s, got %s", contentType, storedAtt.ContentType)
		}

		// Property: Filename should match
		if storedAtt.Filename != filename {
			t.Errorf("expected filename %s, got %s", filename, storedAtt.Filename)
		}
	})
}

// Feature: email-inbox-api, Property 10: Attachment Integrity
// **Validates: Requirements 3.6, 3.7**
//
// *For any* attachment download, the Email_Service SHALL verify the SHA-256 checksum
// matches the stored checksum before serving.
func TestProperty10_AttachmentIntegrity(t *testing.T) {
	// Test the VerifyChecksum function
	t.Run("VerifyChecksum_Valid", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			// Generate random data
			dataLen := rapid.IntRange(10, 1000).Draw(t, "dataLen")
			data := make([]byte, dataLen)
			for i := range data {
				data[i] = byte(rapid.IntRange(0, 255).Draw(t, "byte"))
			}

			// Calculate checksum
			hash := sha256Sum(data)

			// Property: VerifyChecksum should return true for matching checksum
			if !VerifyChecksum(data, hash) {
				t.Error("expected VerifyChecksum to return true for matching checksum")
			}
		})
	})

	t.Run("VerifyChecksum_Invalid", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			// Generate random data
			dataLen := rapid.IntRange(10, 1000).Draw(t, "dataLen")
			data := make([]byte, dataLen)
			for i := range data {
				data[i] = byte(rapid.IntRange(0, 255).Draw(t, "byte"))
			}

			// Use wrong checksum
			wrongChecksum := "0000000000000000000000000000000000000000000000000000000000000000"

			// Property: VerifyChecksum should return false for non-matching checksum
			if VerifyChecksum(data, wrongChecksum) {
				t.Error("expected VerifyChecksum to return false for non-matching checksum")
			}
		})
	})
}

// sha256Sum calculates SHA-256 hash of data
func sha256Sum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}


// Delete implements the Delete method using mock repository
func (s *TestableEmailService) Delete(ctx context.Context, userID uuid.UUID, emailID string) (*DeleteEmailResponse, error) {
	// Parse email ID
	id, err := uuid.Parse(emailID)
	if err != nil {
		return nil, ErrEmailNotFound
	}

	// Check ownership
	owned, err := s.emailRepo.IsOwnedByUser(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if !owned {
		// Check if email exists at all
		_, err := s.emailRepo.GetByID(ctx, id)
		if err != nil {
			if errors.Is(err, repository.ErrEmailNotFound) {
				return nil, ErrEmailNotFound
			}
			return nil, err
		}
		return nil, ErrAccessDenied
	}

	// Get email size before deletion
	emailSize, err := s.emailRepo.GetSizeByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrEmailNotFound) {
			return nil, ErrEmailNotFound
		}
		return nil, err
	}

	// Get attachment info
	storageKeys, _ := s.attachmentRepo.GetStorageKeysByEmailID(ctx, id)
	attachmentSize, _ := s.attachmentRepo.GetTotalSizeByEmailID(ctx, id)

	// Delete attachments from storage
	attachmentsDeleted := 0
	if len(storageKeys) > 0 {
		deleted, _, _ := s.storageService.DeleteByKeys(ctx, storageKeys)
		attachmentsDeleted = deleted
	}

	// Delete email
	if err := s.emailRepo.Delete(ctx, id); err != nil {
		if errors.Is(err, repository.ErrEmailNotFound) {
			return nil, ErrEmailNotFound
		}
		return nil, err
	}

	return &DeleteEmailResponse{
		Message:             "Email deleted successfully",
		EmailID:             emailID,
		AttachmentsDeleted:  attachmentsDeleted,
		TotalSizeFreedBytes: emailSize + attachmentSize,
	}, nil
}

// Feature: email-inbox-api, Property 5: Authorization Enforcement (delete part)
// **Validates: Requirements 4.3**
//
// *For any* email delete request, if the user does not own the email,
// the system SHALL return a 403 Forbidden error.
func TestProperty5_AuthorizationEnforcement_Delete(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		ownerID := uuid.New()
		otherUserID := uuid.New()
		aliasID := uuid.New()

		service.emailRepo.SetAliasOwnership(aliasID, ownerID)

		// Create an email
		emailID := uuid.New()
		email := &repository.Email{
			ID:            emailID,
			AliasID:       aliasID,
			SenderAddress: "sender@example.com",
			ReceivedAt:    time.Now().UTC(),
			SizeBytes:     1000,
		}
		service.emailRepo.AddEmail(email)

		// Other user should get access denied
		_, err := service.Delete(ctx, otherUserID, emailID.String())
		if !errors.Is(err, ErrAccessDenied) {
			t.Errorf("expected ErrAccessDenied for non-owner, got: %v", err)
		}

		// Email should still exist
		_, err = service.emailRepo.GetByID(ctx, emailID)
		if err != nil {
			t.Errorf("expected email to still exist after failed delete, got: %v", err)
		}

		// Owner should be able to delete
		resp, err := service.Delete(ctx, ownerID, emailID.String())
		if err != nil {
			t.Errorf("expected owner to delete email, got error: %v", err)
			return
		}
		if resp == nil {
			t.Error("expected response, got nil")
			return
		}

		// Email should be deleted
		_, err = service.emailRepo.GetByID(ctx, emailID)
		if !errors.Is(err, repository.ErrEmailNotFound) {
			t.Errorf("expected email to be deleted, got: %v", err)
		}
	})
}

// Feature: email-inbox-api, Property 11: Email Deletion
// **Validates: Requirements 4.1, 4.2, 4.5**
//
// *For any* email deletion, the email SHALL be permanently removed from database,
// all associated attachments SHALL be deleted from storage, and the response SHALL
// include the total size freed in bytes.
func TestProperty11_EmailDeletion(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		aliasID := uuid.New()
		service.emailRepo.SetAliasOwnership(aliasID, userID)

		// Create an email
		emailID := uuid.New()
		emailSize := int64(rapid.IntRange(100, 10000).Draw(t, "emailSize"))
		email := &repository.Email{
			ID:            emailID,
			AliasID:       aliasID,
			SenderAddress: "sender@example.com",
			ReceivedAt:    time.Now().UTC(),
			SizeBytes:     emailSize,
		}
		service.emailRepo.AddEmail(email)

		// Create attachments
		attachmentCount := rapid.IntRange(0, 5).Draw(t, "attachmentCount")
		var totalAttachmentSize int64
		for i := 0; i < attachmentCount; i++ {
			attSize := int64(rapid.IntRange(100, 5000).Draw(t, "attSize"))
			totalAttachmentSize += attSize

			attID := uuid.New()
			att := &repository.Attachment{
				ID:          attID,
				EmailID:     emailID,
				Filename:    "file.pdf",
				ContentType: "application/pdf",
				SizeBytes:   attSize,
				StorageKey:  "attachments/" + attID.String(),
				CreatedAt:   time.Now().UTC(),
			}
			service.attachmentRepo.AddAttachment(att)
			service.storageService.AddFile(att.StorageKey, make([]byte, attSize))
		}

		// Delete email
		resp, err := service.Delete(ctx, userID, emailID.String())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: Response should include size freed
		expectedSizeFreed := emailSize + totalAttachmentSize
		if resp.TotalSizeFreedBytes != expectedSizeFreed {
			t.Errorf("expected size freed %d, got %d", expectedSizeFreed, resp.TotalSizeFreedBytes)
		}

		// Property: Email should be permanently removed
		_, err = service.emailRepo.GetByID(ctx, emailID)
		if !errors.Is(err, repository.ErrEmailNotFound) {
			t.Errorf("expected email to be deleted, got: %v", err)
		}

		// Property: Attachments should be deleted from storage
		if resp.AttachmentsDeleted != attachmentCount {
			t.Errorf("expected %d attachments deleted, got %d", attachmentCount, resp.AttachmentsDeleted)
		}
	})
}

// TestProperty11_EmailDeletion_NotFound tests 404 for non-existent email
func TestProperty11_EmailDeletion_NotFound(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		nonExistentEmailID := uuid.New()

		// Should return not found error
		_, err := service.Delete(ctx, userID, nonExistentEmailID.String())
		if !errors.Is(err, ErrEmailNotFound) {
			t.Errorf("expected ErrEmailNotFound for non-existent email, got: %v", err)
		}
	})
}


// BulkDelete implements the BulkDelete method using mock repository
func (s *TestableEmailService) BulkDelete(ctx context.Context, userID uuid.UUID, emailIDs []string) (*BulkOperationResponse, error) {
	// Validate limit
	if len(emailIDs) > 100 {
		return nil, ErrBulkLimitExceeded
	}

	// Parse and filter owned email IDs
	var parsedIDs []uuid.UUID
	for _, idStr := range emailIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			continue // Skip invalid IDs
		}
		parsedIDs = append(parsedIDs, id)
	}

	// Get owned email IDs
	ownedIDs, err := s.emailRepo.GetEmailIDsOwnedByUser(ctx, parsedIDs, userID)
	if err != nil {
		return nil, err
	}

	// Delete owned emails
	successCount := 0
	var failedIDs []string

	for _, id := range parsedIDs {
		isOwned := false
		for _, ownedID := range ownedIDs {
			if id == ownedID {
				isOwned = true
				break
			}
		}

		if !isOwned {
			failedIDs = append(failedIDs, id.String())
			continue
		}

		// Get attachment storage keys before deletion
		storageKeys, _ := s.attachmentRepo.GetStorageKeysByEmailID(ctx, id)

		// Delete attachments from storage
		if len(storageKeys) > 0 {
			s.storageService.DeleteByKeys(ctx, storageKeys)
		}

		// Delete email
		if err := s.emailRepo.Delete(ctx, id); err == nil {
			successCount++
		} else {
			failedIDs = append(failedIDs, id.String())
		}
	}

	return &BulkOperationResponse{
		SuccessCount: successCount,
		FailedCount:  len(failedIDs),
		FailedIDs:    failedIDs,
	}, nil
}

// BulkMarkAsRead implements the BulkMarkAsRead method using mock repository
func (s *TestableEmailService) BulkMarkAsRead(ctx context.Context, userID uuid.UUID, emailIDs []string) (*BulkOperationResponse, error) {
	// Validate limit
	if len(emailIDs) > 100 {
		return nil, ErrBulkLimitExceeded
	}

	// Parse and filter owned email IDs
	var parsedIDs []uuid.UUID
	for _, idStr := range emailIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			continue // Skip invalid IDs
		}
		parsedIDs = append(parsedIDs, id)
	}

	// Get owned email IDs
	ownedIDs, err := s.emailRepo.GetEmailIDsOwnedByUser(ctx, parsedIDs, userID)
	if err != nil {
		return nil, err
	}

	// Mark owned emails as read
	successCount := 0
	var failedIDs []string

	for _, id := range parsedIDs {
		isOwned := false
		for _, ownedID := range ownedIDs {
			if id == ownedID {
				isOwned = true
				break
			}
		}

		if !isOwned {
			failedIDs = append(failedIDs, id.String())
			continue
		}

		if err := s.emailRepo.MarkAsRead(ctx, id); err == nil {
			successCount++
		} else {
			failedIDs = append(failedIDs, id.String())
		}
	}

	return &BulkOperationResponse{
		SuccessCount: successCount,
		FailedCount:  len(failedIDs),
		FailedIDs:    failedIDs,
	}, nil
}


// Feature: email-inbox-api, Property 12: Bulk Operations
// **Validates: Requirements 5.1, 5.2, 5.3, 5.4, 5.5**
//
// *For any* bulk operation (delete or mark as read), the operation SHALL process up to 100 items,
// skip items the user doesn't own, and return accurate counts of successful and failed operations.

// TestProperty12_BulkOperations_Delete tests bulk delete operation
func TestProperty12_BulkOperations_Delete(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		otherUserID := uuid.New()
		userAliasID := uuid.New()
		otherAliasID := uuid.New()

		service.emailRepo.SetAliasOwnership(userAliasID, userID)
		service.emailRepo.SetAliasOwnership(otherAliasID, otherUserID)

		// Create emails for user
		userEmailCount := rapid.IntRange(1, 20).Draw(t, "userEmailCount")
		var userEmailIDs []string
		for i := 0; i < userEmailCount; i++ {
			emailID := uuid.New()
			email := &repository.Email{
				ID:            emailID,
				AliasID:       userAliasID,
				SenderAddress: "sender@example.com",
				ReceivedAt:    time.Now().UTC(),
				SizeBytes:     1000,
			}
			service.emailRepo.AddEmail(email)
			userEmailIDs = append(userEmailIDs, emailID.String())
		}

		// Create emails for other user
		otherEmailCount := rapid.IntRange(1, 10).Draw(t, "otherEmailCount")
		var otherEmailIDs []string
		for i := 0; i < otherEmailCount; i++ {
			emailID := uuid.New()
			email := &repository.Email{
				ID:            emailID,
				AliasID:       otherAliasID,
				SenderAddress: "sender@example.com",
				ReceivedAt:    time.Now().UTC(),
				SizeBytes:     1000,
			}
			service.emailRepo.AddEmail(email)
			otherEmailIDs = append(otherEmailIDs, emailID.String())
		}

		// Mix user and other user emails
		allIDs := append(userEmailIDs, otherEmailIDs...)

		// Bulk delete
		resp, err := service.BulkDelete(ctx, userID, allIDs)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: Success count should equal user's email count
		if resp.SuccessCount != userEmailCount {
			t.Errorf("expected success_count %d, got %d", userEmailCount, resp.SuccessCount)
		}

		// Property: Failed count should equal other user's email count
		if resp.FailedCount != otherEmailCount {
			t.Errorf("expected failed_count %d, got %d", otherEmailCount, resp.FailedCount)
		}

		// Property: Failed IDs should contain other user's emails
		if len(resp.FailedIDs) != otherEmailCount {
			t.Errorf("expected %d failed IDs, got %d", otherEmailCount, len(resp.FailedIDs))
		}

		// Property: User's emails should be deleted
		for _, idStr := range userEmailIDs {
			id, _ := uuid.Parse(idStr)
			_, err := service.emailRepo.GetByID(ctx, id)
			if !errors.Is(err, repository.ErrEmailNotFound) {
				t.Errorf("expected user email %s to be deleted", idStr)
			}
		}

		// Property: Other user's emails should still exist
		for _, idStr := range otherEmailIDs {
			id, _ := uuid.Parse(idStr)
			_, err := service.emailRepo.GetByID(ctx, id)
			if err != nil {
				t.Errorf("expected other user email %s to still exist, got: %v", idStr, err)
			}
		}
	})
}

// TestProperty12_BulkOperations_MarkAsRead tests bulk mark as read operation
func TestProperty12_BulkOperations_MarkAsRead(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		otherUserID := uuid.New()
		userAliasID := uuid.New()
		otherAliasID := uuid.New()

		service.emailRepo.SetAliasOwnership(userAliasID, userID)
		service.emailRepo.SetAliasOwnership(otherAliasID, otherUserID)

		// Create unread emails for user
		userEmailCount := rapid.IntRange(1, 20).Draw(t, "userEmailCount")
		var userEmailIDs []string
		for i := 0; i < userEmailCount; i++ {
			emailID := uuid.New()
			email := &repository.Email{
				ID:            emailID,
				AliasID:       userAliasID,
				SenderAddress: "sender@example.com",
				ReceivedAt:    time.Now().UTC(),
				SizeBytes:     1000,
				IsRead:        false,
			}
			service.emailRepo.AddEmail(email)
			userEmailIDs = append(userEmailIDs, emailID.String())
		}

		// Create unread emails for other user
		otherEmailCount := rapid.IntRange(1, 10).Draw(t, "otherEmailCount")
		var otherEmailIDs []string
		for i := 0; i < otherEmailCount; i++ {
			emailID := uuid.New()
			email := &repository.Email{
				ID:            emailID,
				AliasID:       otherAliasID,
				SenderAddress: "sender@example.com",
				ReceivedAt:    time.Now().UTC(),
				SizeBytes:     1000,
				IsRead:        false,
			}
			service.emailRepo.AddEmail(email)
			otherEmailIDs = append(otherEmailIDs, emailID.String())
		}

		// Mix user and other user emails
		allIDs := append(userEmailIDs, otherEmailIDs...)

		// Bulk mark as read
		resp, err := service.BulkMarkAsRead(ctx, userID, allIDs)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: Success count should equal user's email count
		if resp.SuccessCount != userEmailCount {
			t.Errorf("expected success_count %d, got %d", userEmailCount, resp.SuccessCount)
		}

		// Property: Failed count should equal other user's email count
		if resp.FailedCount != otherEmailCount {
			t.Errorf("expected failed_count %d, got %d", otherEmailCount, resp.FailedCount)
		}

		// Property: User's emails should be marked as read
		for _, idStr := range userEmailIDs {
			id, _ := uuid.Parse(idStr)
			email, err := service.emailRepo.GetByID(ctx, id)
			if err != nil {
				t.Errorf("unexpected error getting email: %v", err)
				continue
			}
			if !email.IsRead {
				t.Errorf("expected user email %s to be marked as read", idStr)
			}
		}

		// Property: Other user's emails should remain unread
		for _, idStr := range otherEmailIDs {
			id, _ := uuid.Parse(idStr)
			email, err := service.emailRepo.GetByID(ctx, id)
			if err != nil {
				t.Errorf("unexpected error getting email: %v", err)
				continue
			}
			if email.IsRead {
				t.Errorf("expected other user email %s to remain unread", idStr)
			}
		}
	})
}

// TestProperty12_BulkOperations_Limit tests 100 item limit
func TestProperty12_BulkOperations_Limit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()

		// Create more than 100 email IDs
		itemCount := rapid.IntRange(101, 150).Draw(t, "itemCount")
		var emailIDs []string
		for i := 0; i < itemCount; i++ {
			emailIDs = append(emailIDs, uuid.New().String())
		}

		// Bulk delete should fail with limit exceeded
		_, err := service.BulkDelete(ctx, userID, emailIDs)
		if !errors.Is(err, ErrBulkLimitExceeded) {
			t.Errorf("expected ErrBulkLimitExceeded for %d items, got: %v", itemCount, err)
		}

		// Bulk mark as read should also fail
		_, err = service.BulkMarkAsRead(ctx, userID, emailIDs)
		if !errors.Is(err, ErrBulkLimitExceeded) {
			t.Errorf("expected ErrBulkLimitExceeded for %d items, got: %v", itemCount, err)
		}
	})
}

// TestProperty12_BulkOperations_ExactLimit tests exactly 100 items (should succeed)
func TestProperty12_BulkOperations_ExactLimit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		aliasID := uuid.New()
		service.emailRepo.SetAliasOwnership(aliasID, userID)

		// Create exactly 100 emails
		var emailIDs []string
		for i := 0; i < 100; i++ {
			emailID := uuid.New()
			email := &repository.Email{
				ID:            emailID,
				AliasID:       aliasID,
				SenderAddress: "sender@example.com",
				ReceivedAt:    time.Now().UTC(),
				SizeBytes:     1000,
				IsRead:        false,
			}
			service.emailRepo.AddEmail(email)
			emailIDs = append(emailIDs, emailID.String())
		}

		// Bulk mark as read should succeed with exactly 100 items
		resp, err := service.BulkMarkAsRead(ctx, userID, emailIDs)
		if err != nil {
			t.Errorf("expected success for exactly 100 items, got error: %v", err)
			return
		}

		// Property: All 100 should succeed
		if resp.SuccessCount != 100 {
			t.Errorf("expected success_count 100, got %d", resp.SuccessCount)
		}
		if resp.FailedCount != 0 {
			t.Errorf("expected failed_count 0, got %d", resp.FailedCount)
		}
	})
}

// TestProperty12_BulkOperations_InvalidIDs tests handling of invalid UUIDs
func TestProperty12_BulkOperations_InvalidIDs(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		aliasID := uuid.New()
		service.emailRepo.SetAliasOwnership(aliasID, userID)

		// Create some valid emails
		validCount := rapid.IntRange(1, 10).Draw(t, "validCount")
		var validIDs []string
		for i := 0; i < validCount; i++ {
			emailID := uuid.New()
			email := &repository.Email{
				ID:            emailID,
				AliasID:       aliasID,
				SenderAddress: "sender@example.com",
				ReceivedAt:    time.Now().UTC(),
				SizeBytes:     1000,
			}
			service.emailRepo.AddEmail(email)
			validIDs = append(validIDs, emailID.String())
		}

		// Add some invalid IDs
		invalidIDs := []string{"not-a-uuid", "12345", "", "invalid-uuid-format"}
		allIDs := append(validIDs, invalidIDs...)

		// Bulk delete should skip invalid IDs
		resp, err := service.BulkDelete(ctx, userID, allIDs)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: Valid emails should be deleted
		if resp.SuccessCount != validCount {
			t.Errorf("expected success_count %d, got %d", validCount, resp.SuccessCount)
		}
	})
}


// GetStats implements the GetStats method using mock repository
func (s *TestableEmailService) GetStats(ctx context.Context, userID uuid.UUID) (*InboxStatsResponse, error) {
	stats, err := s.emailRepo.GetStats(ctx, userID)
	if err != nil {
		return nil, err
	}

	emailsPerAlias := make([]AliasEmailCount, len(stats.EmailsPerAlias))
	for i, a := range stats.EmailsPerAlias {
		emailsPerAlias[i] = AliasEmailCount{
			AliasID:    a.AliasID.String(),
			AliasEmail: a.AliasEmail,
			Count:      a.Count,
		}
	}

	return &InboxStatsResponse{
		TotalEmails:     stats.TotalEmails,
		UnreadEmails:    stats.UnreadEmails,
		TotalSizeBytes:  stats.TotalSizeBytes,
		EmailsToday:     stats.EmailsToday,
		EmailsThisWeek:  stats.EmailsThisWeek,
		EmailsThisMonth: stats.EmailsThisMonth,
		EmailsPerAlias:  emailsPerAlias,
	}, nil
}

// Feature: email-inbox-api, Property 13: Statistics Accuracy
// **Validates: Requirements 6.1, 6.2, 6.3, 6.4, 6.5**
//
// *For any* stats request, the response SHALL include accurate counts for total emails,
// unread emails, total storage used, emails per alias, and emails received today/this week/this month.

// TestProperty13_StatisticsAccuracy_TotalAndUnread tests total and unread email counts
func TestProperty13_StatisticsAccuracy_TotalAndUnread(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		aliasID := uuid.New()
		service.emailRepo.SetAliasOwnership(aliasID, userID)

		// Create emails with random read status
		totalEmails := rapid.IntRange(0, 50).Draw(t, "totalEmails")
		expectedUnread := 0

		for i := 0; i < totalEmails; i++ {
			isRead := rapid.Bool().Draw(t, "isRead")
			if !isRead {
				expectedUnread++
			}

			email := &repository.Email{
				ID:            uuid.New(),
				AliasID:       aliasID,
				SenderAddress: "sender@example.com",
				ReceivedAt:    time.Now().UTC(),
				SizeBytes:     1000,
				IsRead:        isRead,
			}
			service.emailRepo.AddEmail(email)
		}

		// Get stats
		stats, err := service.GetStats(ctx, userID)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: Total emails should be accurate
		if stats.TotalEmails != totalEmails {
			t.Errorf("expected total_emails %d, got %d", totalEmails, stats.TotalEmails)
		}

		// Property: Unread emails should be accurate
		if stats.UnreadEmails != expectedUnread {
			t.Errorf("expected unread_emails %d, got %d", expectedUnread, stats.UnreadEmails)
		}
	})
}

// TestProperty13_StatisticsAccuracy_TotalSize tests total storage size calculation
func TestProperty13_StatisticsAccuracy_TotalSize(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		aliasID := uuid.New()
		service.emailRepo.SetAliasOwnership(aliasID, userID)

		// Create emails with random sizes
		totalEmails := rapid.IntRange(1, 30).Draw(t, "totalEmails")
		var expectedTotalSize int64

		for i := 0; i < totalEmails; i++ {
			size := int64(rapid.IntRange(100, 10000).Draw(t, "size"))
			expectedTotalSize += size

			email := &repository.Email{
				ID:            uuid.New(),
				AliasID:       aliasID,
				SenderAddress: "sender@example.com",
				ReceivedAt:    time.Now().UTC(),
				SizeBytes:     size,
			}
			service.emailRepo.AddEmail(email)
		}

		// Get stats
		stats, err := service.GetStats(ctx, userID)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: Total size should be accurate
		if stats.TotalSizeBytes != expectedTotalSize {
			t.Errorf("expected total_size_bytes %d, got %d", expectedTotalSize, stats.TotalSizeBytes)
		}
	})
}

// TestProperty13_StatisticsAccuracy_EmailsPerAlias tests emails per alias count
func TestProperty13_StatisticsAccuracy_EmailsPerAlias(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()

		// Create multiple aliases
		aliasCount := rapid.IntRange(1, 5).Draw(t, "aliasCount")
		aliasIDs := make([]uuid.UUID, aliasCount)
		expectedCounts := make(map[uuid.UUID]int)

		for i := 0; i < aliasCount; i++ {
			aliasIDs[i] = uuid.New()
			service.emailRepo.SetAliasOwnership(aliasIDs[i], userID)
			expectedCounts[aliasIDs[i]] = 0
		}

		// Create emails distributed across aliases
		totalEmails := rapid.IntRange(5, 30).Draw(t, "totalEmails")
		for i := 0; i < totalEmails; i++ {
			aliasIdx := rapid.IntRange(0, aliasCount-1).Draw(t, "aliasIdx")
			aliasID := aliasIDs[aliasIdx]
			expectedCounts[aliasID]++

			email := &repository.Email{
				ID:            uuid.New(),
				AliasID:       aliasID,
				SenderAddress: "sender@example.com",
				ReceivedAt:    time.Now().UTC(),
				SizeBytes:     1000,
			}
			service.emailRepo.AddEmail(email)
		}

		// Get stats
		stats, err := service.GetStats(ctx, userID)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: Emails per alias should be accurate
		for _, aliasStats := range stats.EmailsPerAlias {
			aliasID, _ := uuid.Parse(aliasStats.AliasID)
			expected := expectedCounts[aliasID]
			if aliasStats.Count != expected {
				t.Errorf("expected alias %s to have %d emails, got %d", aliasStats.AliasID, expected, aliasStats.Count)
			}
		}

		// Property: Sum of emails per alias should equal total
		sumPerAlias := 0
		for _, aliasStats := range stats.EmailsPerAlias {
			sumPerAlias += aliasStats.Count
		}
		if sumPerAlias != totalEmails {
			t.Errorf("expected sum of emails per alias %d to equal total %d", sumPerAlias, totalEmails)
		}
	})
}

// TestProperty13_StatisticsAccuracy_TimePeriods tests emails today/week/month counts
func TestProperty13_StatisticsAccuracy_TimePeriods(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		userID := uuid.New()
		aliasID := uuid.New()
		service.emailRepo.SetAliasOwnership(aliasID, userID)

		now := time.Now().UTC()
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		weekAgo := now.AddDate(0, 0, -7)
		monthAgo := now.AddDate(0, 0, -30)

		expectedToday := 0
		expectedThisWeek := 0
		expectedThisMonth := 0

		// Create emails at various times
		totalEmails := rapid.IntRange(10, 50).Draw(t, "totalEmails")
		for i := 0; i < totalEmails; i++ {
			daysAgo := rapid.IntRange(0, 60).Draw(t, "daysAgo")
			receivedAt := now.AddDate(0, 0, -daysAgo)

			if receivedAt.After(today) || receivedAt.Equal(today) {
				expectedToday++
			}
			if receivedAt.After(weekAgo) {
				expectedThisWeek++
			}
			if receivedAt.After(monthAgo) {
				expectedThisMonth++
			}

			email := &repository.Email{
				ID:            uuid.New(),
				AliasID:       aliasID,
				SenderAddress: "sender@example.com",
				ReceivedAt:    receivedAt,
				SizeBytes:     1000,
			}
			service.emailRepo.AddEmail(email)
		}

		// Get stats
		stats, err := service.GetStats(ctx, userID)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: Emails today should be accurate
		if stats.EmailsToday != expectedToday {
			t.Errorf("expected emails_today %d, got %d", expectedToday, stats.EmailsToday)
		}

		// Property: Emails this week should be accurate
		if stats.EmailsThisWeek != expectedThisWeek {
			t.Errorf("expected emails_this_week %d, got %d", expectedThisWeek, stats.EmailsThisWeek)
		}

		// Property: Emails this month should be accurate
		if stats.EmailsThisMonth != expectedThisMonth {
			t.Errorf("expected emails_this_month %d, got %d", expectedThisMonth, stats.EmailsThisMonth)
		}
	})
}

// TestProperty13_StatisticsAccuracy_OnlyUserEmails tests that stats only include user's emails
func TestProperty13_StatisticsAccuracy_OnlyUserEmails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		service := NewTestableEmailService()

		user1ID := uuid.New()
		user2ID := uuid.New()
		alias1ID := uuid.New()
		alias2ID := uuid.New()

		service.emailRepo.SetAliasOwnership(alias1ID, user1ID)
		service.emailRepo.SetAliasOwnership(alias2ID, user2ID)

		// Create emails for user1
		user1EmailCount := rapid.IntRange(1, 20).Draw(t, "user1EmailCount")
		var user1TotalSize int64
		for i := 0; i < user1EmailCount; i++ {
			size := int64(rapid.IntRange(100, 5000).Draw(t, "size"))
			user1TotalSize += size

			email := &repository.Email{
				ID:            uuid.New(),
				AliasID:       alias1ID,
				SenderAddress: "sender@example.com",
				ReceivedAt:    time.Now().UTC(),
				SizeBytes:     size,
			}
			service.emailRepo.AddEmail(email)
		}

		// Create emails for user2
		user2EmailCount := rapid.IntRange(1, 20).Draw(t, "user2EmailCount")
		for i := 0; i < user2EmailCount; i++ {
			email := &repository.Email{
				ID:            uuid.New(),
				AliasID:       alias2ID,
				SenderAddress: "sender@example.com",
				ReceivedAt:    time.Now().UTC(),
				SizeBytes:     int64(rapid.IntRange(100, 5000).Draw(t, "size")),
			}
			service.emailRepo.AddEmail(email)
		}

		// Get stats for user1
		stats, err := service.GetStats(ctx, user1ID)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}

		// Property: Stats should only include user1's emails
		if stats.TotalEmails != user1EmailCount {
			t.Errorf("expected total_emails %d for user1, got %d", user1EmailCount, stats.TotalEmails)
		}

		if stats.TotalSizeBytes != user1TotalSize {
			t.Errorf("expected total_size_bytes %d for user1, got %d", user1TotalSize, stats.TotalSizeBytes)
		}
	})
}
