package alias

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/domain"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/events"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/repository"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/storage"
)

const (
	// DefaultAliasLimit is the default max aliases per user
	DefaultAliasLimit = 50
)

// Service errors
var (
	ErrAliasNotFound      = errors.New("alias not found")
	ErrAliasExists        = errors.New("alias already exists")
	ErrAliasLimitReached  = errors.New("alias limit reached")
	ErrDomainNotFound     = errors.New("domain not found")
	ErrDomainNotVerified  = errors.New("domain not verified")
	ErrAccessDenied       = errors.New("access denied")
	ErrValidationFailed   = errors.New("validation failed")
)

// Error codes for API responses
const (
	CodeValidationError    = "VALIDATION_ERROR"
	CodeAliasNotFound      = "ALIAS_NOT_FOUND"
	CodeDomainNotFound     = "DOMAIN_NOT_FOUND"
	CodeForbidden          = "FORBIDDEN"
	CodeDomainNotVerified  = "DOMAIN_NOT_VERIFIED"
	CodeAliasExists        = "ALIAS_EXISTS"
	CodeAliasLimitReached  = "ALIAS_LIMIT_REACHED"
)

// CreateAliasRequest represents the request to create an alias
type CreateAliasRequest struct {
	LocalPart   string `json:"local_part" validate:"required,min=1,max=64"`
	DomainID    string `json:"domain_id" validate:"required,uuid"`
	Description string `json:"description,omitempty" validate:"omitempty,max=500"`
}

// UpdateAliasRequest represents the request to update an alias
type UpdateAliasRequest struct {
	IsActive    *bool   `json:"is_active,omitempty"`
	Description *string `json:"description,omitempty" validate:"omitempty,max=500"`
}

// ListAliasParams holds parameters for listing aliases
type ListAliasParams struct {
	Page     int    `json:"page" validate:"min=1"`
	Limit    int    `json:"limit" validate:"min=1,max=100"`
	DomainID string `json:"domain_id,omitempty" validate:"omitempty,uuid"`
	Search   string `json:"search,omitempty" validate:"omitempty,max=64"`
	Sort     string `json:"sort,omitempty" validate:"omitempty,oneof=created_at email_count"`
	Order    string `json:"order,omitempty" validate:"omitempty,oneof=asc desc"`
}


// AliasResponse represents the alias data in responses
type AliasResponse struct {
	ID                  string     `json:"id"`
	EmailAddress        string     `json:"email_address"`
	LocalPart           string     `json:"local_part"`
	DomainID            string     `json:"domain_id"`
	DomainName          string     `json:"domain_name"`
	Description         *string    `json:"description,omitempty"`
	IsActive            bool       `json:"is_active"`
	CreatedAt           time.Time  `json:"created_at"`
	EmailCount          int        `json:"email_count"`
	LastEmailReceivedAt *time.Time `json:"last_email_received_at,omitempty"`
	TotalSizeBytes      int64      `json:"total_size_bytes"`
}

// AliasDetailResponse represents detailed alias data with stats
type AliasDetailResponse struct {
	AliasResponse
	Stats AliasStats `json:"stats"`
}

// AliasStats represents detailed statistics for an alias
type AliasStats struct {
	EmailsToday     int         `json:"emails_today"`
	EmailsThisWeek  int         `json:"emails_this_week"`
	EmailsThisMonth int         `json:"emails_this_month"`
	TopSenders      []TopSender `json:"top_senders"`
}

// TopSender represents a frequent sender for an alias
type TopSender struct {
	Email string `json:"email"`
	Count int    `json:"count"`
}

// AliasListResponse represents the paginated list of aliases
type AliasListResponse struct {
	Aliases    []AliasResponse `json:"aliases"`
	Pagination Pagination      `json:"pagination"`
}

// Pagination represents pagination metadata
type Pagination struct {
	CurrentPage int `json:"current_page"`
	PerPage     int `json:"per_page"`
	TotalPages  int `json:"total_pages"`
	TotalCount  int `json:"total_count"`
}

// DeleteAliasResponse represents the response after deleting an alias
type DeleteAliasResponse struct {
	Message             string `json:"message"`
	AliasID             string `json:"alias_id"`
	EmailAddress        string `json:"email_address"`
	EmailsDeleted       int    `json:"emails_deleted"`
	AttachmentsDeleted  int    `json:"attachments_deleted"`
	TotalSizeFreedBytes int64  `json:"total_size_freed_bytes"`
}

// Service handles alias business logic
type Service struct {
	aliasRepo      *repository.AliasRepository
	domainRepo     domain.Repository
	storageService *storage.StorageService
	eventBus       events.EventBus
	aliasLimit     int
	logger         *slog.Logger
}

// ServiceConfig contains configuration for the alias Service
type ServiceConfig struct {
	AliasRepository *repository.AliasRepository
	DomainRepo      domain.Repository
	StorageService  *storage.StorageService
	EventBus        events.EventBus
	AliasLimit      int // Max aliases per user (default: 50)
	Logger          *slog.Logger
}

// NewService creates a new alias Service instance
func NewService(cfg ServiceConfig) *Service {
	if cfg.AliasLimit <= 0 {
		cfg.AliasLimit = DefaultAliasLimit
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &Service{
		aliasRepo:      cfg.AliasRepository,
		domainRepo:     cfg.DomainRepo,
		storageService: cfg.StorageService,
		eventBus:       cfg.EventBus,
		aliasLimit:     cfg.AliasLimit,
		logger:         cfg.Logger,
	}
}


// Create creates a new email alias
// Requirements: 1.1-1.9, 6.1-6.5, 7.1, 7.3
func (s *Service) Create(ctx context.Context, userID uuid.UUID, req CreateAliasRequest) (*AliasResponse, []string, error) {
	// Validate local_part (Requirements: 1.2, 1.8, 6.1-6.5)
	validationErrors := ValidateLocalPart(req.LocalPart)
	if len(validationErrors) > 0 {
		return nil, validationErrors, ErrValidationFailed
	}

	// Parse domain ID
	domainID, err := uuid.Parse(req.DomainID)
	if err != nil {
		return nil, []string{"invalid domain_id format"}, ErrValidationFailed
	}

	// Check domain exists (Requirement: 1.4)
	domainEntity, err := s.domainRepo.GetByID(ctx, domainID)
	if err != nil {
		if errors.Is(err, domain.ErrDomainNotFound) {
			return nil, nil, ErrDomainNotFound
		}
		return nil, nil, fmt.Errorf("failed to get domain: %w", err)
	}

	// Check domain ownership (Requirement: 1.5)
	if domainEntity.UserID != userID {
		return nil, nil, ErrAccessDenied
	}

	// Check domain is verified (Requirement: 1.6)
	if !domainEntity.IsVerified {
		return nil, nil, ErrDomainNotVerified
	}

	// Generate full address (Requirement: 1.9, 6.5)
	fullAddress := GenerateFullAddress(req.LocalPart, domainEntity.DomainName)

	// Check alias uniqueness (Requirement: 1.7, 7.3)
	exists, err := s.aliasRepo.ExistsByFullAddress(ctx, fullAddress)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to check alias existence: %w", err)
	}
	if exists {
		return nil, nil, ErrAliasExists
	}

	// Check alias limit (Requirement: 7.1)
	count, err := s.aliasRepo.CountByUserID(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to count user aliases: %w", err)
	}
	if count >= s.aliasLimit {
		return nil, nil, ErrAliasLimitReached
	}

	// Create alias (Requirement: 1.1)
	var description *string
	if req.Description != "" {
		description = &req.Description
	}

	alias := &repository.Alias{
		ID:          uuid.New(),
		UserID:      userID,
		DomainID:    domainID,
		LocalPart:   req.LocalPart,
		FullAddress: fullAddress,
		Description: description,
		IsActive:    true,
	}

	if err := s.aliasRepo.Create(ctx, alias); err != nil {
		if errors.Is(err, repository.ErrAliasExists) {
			return nil, nil, ErrAliasExists
		}
		return nil, nil, fmt.Errorf("failed to create alias: %w", err)
	}

	s.logger.Info("Alias created",
		"alias_id", alias.ID,
		"full_address", fullAddress,
		"user_id", userID,
		"domain_id", domainID,
	)

	// Publish alias_created event
	// Requirements: 5.1, 5.2 - Real-time notification for alias creation
	if s.eventBus != nil {
		s.publishAliasCreatedEvent(userID.String(), alias.ID.String(), fullAddress, domainID.String(), alias.CreatedAt)
	}

	return &AliasResponse{
		ID:                  alias.ID.String(),
		EmailAddress:        alias.FullAddress,
		LocalPart:           alias.LocalPart,
		DomainID:            alias.DomainID.String(),
		DomainName:          domainEntity.DomainName,
		Description:         alias.Description,
		IsActive:            alias.IsActive,
		CreatedAt:           alias.CreatedAt,
		EmailCount:          0,
		LastEmailReceivedAt: nil,
		TotalSizeBytes:      0,
	}, nil, nil
}


// List retrieves aliases for a user with pagination, filtering, search, and sorting
// Requirements: 2.1-2.6
func (s *Service) List(ctx context.Context, userID uuid.UUID, params ListAliasParams) (*AliasListResponse, error) {
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
	repoParams := repository.ListAliasParams{
		Page:   params.Page,
		Limit:  params.Limit,
		Search: params.Search,
		Sort:   params.Sort,
		Order:  params.Order,
	}

	// Parse domain filter if provided
	if params.DomainID != "" {
		domainID, err := uuid.Parse(params.DomainID)
		if err != nil {
			return nil, fmt.Errorf("invalid domain_id format: %w", err)
		}
		repoParams.DomainID = &domainID
	}

	// Get aliases from repository
	aliases, totalCount, err := s.aliasRepo.List(ctx, userID, repoParams)
	if err != nil {
		return nil, fmt.Errorf("failed to list aliases: %w", err)
	}

	// Convert to response format
	aliasResponses := make([]AliasResponse, len(aliases))
	for i, a := range aliases {
		aliasResponses[i] = AliasResponse{
			ID:                  a.ID.String(),
			EmailAddress:        a.FullAddress,
			LocalPart:           a.LocalPart,
			DomainID:            a.DomainID.String(),
			DomainName:          a.DomainName,
			Description:         a.Description,
			IsActive:            a.IsActive,
			CreatedAt:           a.CreatedAt,
			EmailCount:          a.EmailCount,
			LastEmailReceivedAt: a.LastEmailReceivedAt,
			TotalSizeBytes:      a.TotalSizeBytes,
		}
	}

	// Calculate pagination
	totalPages := (totalCount + params.Limit - 1) / params.Limit
	if totalPages < 1 {
		totalPages = 1
	}

	return &AliasListResponse{
		Aliases: aliasResponses,
		Pagination: Pagination{
			CurrentPage: params.Page,
			PerPage:     params.Limit,
			TotalPages:  totalPages,
			TotalCount:  totalCount,
		},
	}, nil
}

// GetByID retrieves an alias by ID with ownership check and detailed stats
// Requirements: 3.1-3.4
func (s *Service) GetByID(ctx context.Context, userID uuid.UUID, aliasID string) (*AliasDetailResponse, error) {
	// Parse alias ID
	id, err := uuid.Parse(aliasID)
	if err != nil {
		return nil, ErrAliasNotFound
	}

	// Get alias from repository
	alias, err := s.aliasRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrAliasNotFound) {
			return nil, ErrAliasNotFound
		}
		return nil, fmt.Errorf("failed to get alias: %w", err)
	}

	// Check ownership (Requirement: 3.3)
	if alias.UserID != userID {
		return nil, ErrAccessDenied
	}

	// Get detailed stats (Requirement: 3.4)
	stats, err := s.aliasRepo.GetDetailStats(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get alias stats: %w", err)
	}

	return &AliasDetailResponse{
		AliasResponse: AliasResponse{
			ID:                  alias.ID.String(),
			EmailAddress:        alias.FullAddress,
			LocalPart:           alias.LocalPart,
			DomainID:            alias.DomainID.String(),
			DomainName:          alias.DomainName,
			Description:         alias.Description,
			IsActive:            alias.IsActive,
			CreatedAt:           alias.CreatedAt,
			EmailCount:          alias.EmailCount,
			LastEmailReceivedAt: alias.LastEmailReceivedAt,
			TotalSizeBytes:      alias.TotalSizeBytes,
		},
		Stats: AliasStats{
			EmailsToday:     stats.EmailsToday,
			EmailsThisWeek:  stats.EmailsThisWeek,
			EmailsThisMonth: stats.EmailsThisMonth,
			TopSenders:      convertTopSenders(stats.TopSenders),
		},
	}, nil
}


// Update updates an alias's is_active and description fields
// Requirements: 4.1-4.5
func (s *Service) Update(ctx context.Context, userID uuid.UUID, aliasID string, req UpdateAliasRequest) (*AliasResponse, error) {
	// Parse alias ID
	id, err := uuid.Parse(aliasID)
	if err != nil {
		return nil, ErrAliasNotFound
	}

	// Get alias from repository
	alias, err := s.aliasRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrAliasNotFound) {
			return nil, ErrAliasNotFound
		}
		return nil, fmt.Errorf("failed to get alias: %w", err)
	}

	// Check ownership (Requirement: 4.4)
	if alias.UserID != userID {
		return nil, ErrAccessDenied
	}

	// Build update model
	updateAlias := &repository.Alias{
		ID:          alias.ID,
		IsActive:    alias.IsActive,
		Description: alias.Description,
	}

	// Apply updates (Requirements: 4.1, 4.2)
	if req.IsActive != nil {
		updateAlias.IsActive = *req.IsActive
	}
	if req.Description != nil {
		if *req.Description == "" {
			updateAlias.Description = nil
		} else {
			updateAlias.Description = req.Description
		}
	}

	// Update in repository (Requirement: 4.5 - updated_at is handled by repository)
	if err := s.aliasRepo.Update(ctx, updateAlias); err != nil {
		if errors.Is(err, repository.ErrAliasNotFound) {
			return nil, ErrAliasNotFound
		}
		return nil, fmt.Errorf("failed to update alias: %w", err)
	}

	s.logger.Info("Alias updated",
		"alias_id", alias.ID,
		"user_id", userID,
	)

	return &AliasResponse{
		ID:                  alias.ID.String(),
		EmailAddress:        alias.FullAddress,
		LocalPart:           alias.LocalPart,
		DomainID:            alias.DomainID.String(),
		DomainName:          alias.DomainName,
		Description:         updateAlias.Description,
		IsActive:            updateAlias.IsActive,
		CreatedAt:           alias.CreatedAt,
		EmailCount:          alias.EmailCount,
		LastEmailReceivedAt: alias.LastEmailReceivedAt,
		TotalSizeBytes:      alias.TotalSizeBytes,
	}, nil
}

// Delete deletes an alias with cascade delete of emails and attachments
// Requirements: 5.1-5.5
func (s *Service) Delete(ctx context.Context, userID uuid.UUID, aliasID string) (*DeleteAliasResponse, error) {
	// Parse alias ID
	id, err := uuid.Parse(aliasID)
	if err != nil {
		return nil, ErrAliasNotFound
	}

	// Get alias from repository
	alias, err := s.aliasRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrAliasNotFound) {
			return nil, ErrAliasNotFound
		}
		return nil, fmt.Errorf("failed to get alias: %w", err)
	}

	// Check ownership (Requirement: 5.4)
	if alias.UserID != userID {
		return nil, ErrAccessDenied
	}

	// Get delete info before deletion (Requirement: 5.3)
	emailCount, attachmentCount, totalSize, err := s.aliasRepo.GetDeleteInfo(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get delete info: %w", err)
	}

	// Delete attachments from storage (Requirement: 5.2)
	if s.storageService != nil && attachmentCount > 0 {
		storageKeys, err := s.aliasRepo.GetAttachmentStorageKeys(ctx, id)
		if err != nil {
			s.logger.Warn("Failed to get attachment storage keys", "alias_id", id, "error", err)
		} else if len(storageKeys) > 0 {
			_, _, err := s.storageService.DeleteByKeys(ctx, storageKeys)
			if err != nil {
				s.logger.Warn("Failed to delete attachments from storage", "alias_id", id, "error", err)
				// Continue with deletion even if storage cleanup fails
			}
		}
	}

	// Delete alias (cascade handles emails and attachments in DB) (Requirement: 5.1)
	if err := s.aliasRepo.Delete(ctx, id); err != nil {
		if errors.Is(err, repository.ErrAliasNotFound) {
			return nil, ErrAliasNotFound
		}
		return nil, fmt.Errorf("failed to delete alias: %w", err)
	}

	s.logger.Info("Alias deleted",
		"alias_id", id,
		"full_address", alias.FullAddress,
		"user_id", userID,
		"emails_deleted", emailCount,
		"attachments_deleted", attachmentCount,
	)

	// Publish alias_deleted event
	// Requirements: 5.3, 5.4 - Real-time notification for alias deletion
	if s.eventBus != nil {
		s.publishAliasDeletedEvent(userID.String(), alias.ID.String(), alias.FullAddress, time.Now().UTC(), emailCount)
	}

	return &DeleteAliasResponse{
		Message:             "Alias deleted successfully",
		AliasID:             alias.ID.String(),
		EmailAddress:        alias.FullAddress,
		EmailsDeleted:       emailCount,
		AttachmentsDeleted:  attachmentCount,
		TotalSizeFreedBytes: totalSize,
	}, nil
}


// CountByUserID returns the number of aliases owned by a user
// Requirements: 7.2
func (s *Service) CountByUserID(ctx context.Context, userID uuid.UUID) (int, error) {
	return s.aliasRepo.CountByUserID(ctx, userID)
}

// GetAliasLimit returns the alias limit for users
func (s *Service) GetAliasLimit() int {
	return s.aliasLimit
}

// convertTopSenders converts repository TopSender to service TopSender
func convertTopSenders(repoSenders []repository.TopSender) []TopSender {
	senders := make([]TopSender, len(repoSenders))
	for i, s := range repoSenders {
		senders[i] = TopSender{
			Email: s.Email,
			Count: s.Count,
		}
	}
	return senders
}


// publishAliasCreatedEvent publishes an alias_created event to the event bus
// Requirements: 5.1, 5.2 - Real-time notification for alias creation
func (s *Service) publishAliasCreatedEvent(userID, aliasID, emailAddress, domainID string, createdAt time.Time) {
	eventData := events.AliasCreatedEvent{
		ID:           aliasID,
		EmailAddress: emailAddress,
		DomainID:     domainID,
		CreatedAt:    createdAt,
	}

	data, err := json.Marshal(eventData)
	if err != nil {
		s.logger.Warn("Failed to marshal alias_created event", "error", err)
		return
	}

	event := events.Event{
		ID:        generateEventID(),
		Type:      events.EventTypeAliasCreated,
		UserID:    userID,
		Data:      data,
		Timestamp: time.Now().UTC(),
	}

	if err := s.eventBus.Publish(event); err != nil {
		s.logger.Warn("Failed to publish alias_created event", "alias_id", aliasID, "error", err)
	}
}

// publishAliasDeletedEvent publishes an alias_deleted event to the event bus
// Requirements: 5.3, 5.4 - Real-time notification for alias deletion
func (s *Service) publishAliasDeletedEvent(userID, aliasID, emailAddress string, deletedAt time.Time, emailsDeleted int) {
	eventData := events.AliasDeletedEvent{
		ID:            aliasID,
		EmailAddress:  emailAddress,
		DeletedAt:     deletedAt,
		EmailsDeleted: emailsDeleted,
	}

	data, err := json.Marshal(eventData)
	if err != nil {
		s.logger.Warn("Failed to marshal alias_deleted event", "error", err)
		return
	}

	event := events.Event{
		ID:        generateEventID(),
		Type:      events.EventTypeAliasDeleted,
		UserID:    userID,
		Data:      data,
		Timestamp: time.Now().UTC(),
	}

	if err := s.eventBus.Publish(event); err != nil {
		s.logger.Warn("Failed to publish alias_deleted event", "alias_id", aliasID, "error", err)
	}
}

// generateEventID generates a unique event ID
func generateEventID() string {
	return uuid.New().String()
}
