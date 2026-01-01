package api

import (
	"time"

	"github.com/google/uuid"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/domain"
)

// CreateDomainRequest represents the request body for creating a domain
type CreateDomainRequest struct {
	DomainName string `json:"domain_name" validate:"required,min=4,max=253"`
}

// DomainResponse represents a domain in API responses
type DomainResponse struct {
	ID                uuid.UUID        `json:"id"`
	DomainName        string           `json:"domain_name"`
	Status            string           `json:"status"` // "pending" or "verified"
	VerificationToken string           `json:"verification_token,omitempty"`
	MXRecordConfigured bool            `json:"mx_record_configured"`
	SSLStatus         string           `json:"ssl_status"` // "pending", "active", "expired"
	SSLExpiresAt      *time.Time       `json:"ssl_expires_at,omitempty"`
	AliasCount        int              `json:"alias_count"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
	VerifiedAt        *time.Time       `json:"verified_at,omitempty"`
	DNSInstructions   *DNSInstructions `json:"dns_instructions,omitempty"`
}

// DNSInstructions contains DNS setup instructions
type DNSInstructions struct {
	MXRecord  MXRecordInstruction  `json:"mx_record"`
	TXTRecord TXTRecordInstruction `json:"txt_record"`
}

// MXRecordInstruction contains MX record setup details
type MXRecordInstruction struct {
	Type     string `json:"type"`
	Priority int    `json:"priority"`
	Value    string `json:"value"`
}

// TXTRecordInstruction contains TXT record setup details
type TXTRecordInstruction struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ListDomainsResponse represents the response for listing domains
type ListDomainsResponse struct {
	Domains    []DomainResponse `json:"domains"`
	Pagination PaginationInfo   `json:"pagination"`
}

// PaginationInfo contains pagination metadata
type PaginationInfo struct {
	CurrentPage int `json:"current_page"`
	PerPage     int `json:"per_page"`
	TotalPages  int `json:"total_pages"`
	TotalCount  int `json:"total_count"`
}

// DeleteDomainResponse represents the response for deleting a domain
type DeleteDomainResponse struct {
	DomainID           uuid.UUID `json:"domain_id"`
	AliasesDeleted     int       `json:"aliases_deleted"`
	EmailsDeleted      int       `json:"emails_deleted"`
	AttachmentsDeleted int       `json:"attachments_deleted"`
}

// VerifyDomainResponse represents the response for domain verification
type VerifyDomainResponse struct {
	Domain              DomainResponse      `json:"domain"`
	VerificationDetails VerificationDetails `json:"verification_details"`
}

// VerificationDetails contains DNS verification results
type VerificationDetails struct {
	MXRecordsFound       []MXRecordFound `json:"mx_records_found"`
	TXTRecordFound       bool            `json:"txt_record_found"`
	SSLCertificateIssued bool            `json:"ssl_certificate_issued"`
	Issues               []string        `json:"issues,omitempty"`
}

// MXRecordFound represents a found MX record
type MXRecordFound struct {
	Priority int    `json:"priority"`
	Hostname string `json:"hostname"`
	IsValid  bool   `json:"is_valid"`
}

// DNSStatusResponse represents the response for DNS status check
type DNSStatusResponse struct {
	Domain    DomainResponse `json:"domain"`
	DNSStatus DNSStatus      `json:"dns_status"`
}

// DNSStatus contains current DNS configuration status
type DNSStatus struct {
	MXRecords       []MXRecordFound `json:"mx_records"`
	TXTRecords      []TXTRecordFound `json:"txt_records"`
	MXValid         bool            `json:"mx_valid"`
	TXTValid        bool            `json:"txt_valid"`
	IsReadyToVerify bool            `json:"is_ready_to_verify"`
	Issues          []string        `json:"issues,omitempty"`
}

// TXTRecordFound represents a found TXT record
type TXTRecordFound struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsValid bool   `json:"is_valid"`
}


// Helper functions to convert domain entities to DTOs

// ToDomainResponse converts a domain entity to a response DTO
func ToDomainResponse(d *domain.Domain, instructions *domain.DNSInstructions) DomainResponse {
	resp := DomainResponse{
		ID:                 d.ID,
		DomainName:         d.DomainName,
		Status:             getDomainStatus(d),
		MXRecordConfigured: d.IsVerified,
		SSLStatus:          getSSLStatus(d),
		SSLExpiresAt:       d.SSLExpiresAt,
		AliasCount:         d.AliasCount,
		CreatedAt:          d.CreatedAt,
		UpdatedAt:          d.UpdatedAt,
		VerifiedAt:         d.VerifiedAt,
	}

	// Include verification token only for pending domains
	if !d.IsVerified {
		resp.VerificationToken = d.VerificationToken
	}

	// Include DNS instructions if provided
	if instructions != nil {
		resp.DNSInstructions = &DNSInstructions{
			MXRecord: MXRecordInstruction{
				Type:     instructions.MXRecord.Type,
				Priority: instructions.MXRecord.Priority,
				Value:    instructions.MXRecord.Value,
			},
			TXTRecord: TXTRecordInstruction{
				Type:  instructions.TXTRecord.Type,
				Name:  instructions.TXTRecord.Name,
				Value: instructions.TXTRecord.Value,
			},
		}
	}

	return resp
}

// getDomainStatus returns the status string for a domain
func getDomainStatus(d *domain.Domain) string {
	if d.IsVerified {
		return "verified"
	}
	return "pending"
}

// getSSLStatus returns the SSL status string for a domain
// Returns: "pending", "provisioning", "active", "expired", "failed", "revoked"
func getSSLStatus(d *domain.Domain) string {
	if !d.SSLEnabled {
		return "pending"
	}
	if d.SSLExpiresAt != nil && d.SSLExpiresAt.Before(time.Now()) {
		return "expired"
	}
	return "active"
}

// ToDeleteDomainResponse converts a delete result to a response DTO
func ToDeleteDomainResponse(result *domain.DeleteResult) DeleteDomainResponse {
	return DeleteDomainResponse{
		DomainID:           result.DomainID,
		AliasesDeleted:     result.AliasesDeleted,
		EmailsDeleted:      result.EmailsDeleted,
		AttachmentsDeleted: result.AttachmentsDeleted,
	}
}

// ToVerificationDetails converts DNS check result to verification details
func ToVerificationDetails(dnsResult *domain.DNSCheckResult, sslIssued bool) VerificationDetails {
	details := VerificationDetails{
		MXRecordsFound:       make([]MXRecordFound, 0, len(dnsResult.MXRecords)),
		TXTRecordFound:       dnsResult.TXTValid,
		SSLCertificateIssued: sslIssued,
		Issues:               dnsResult.Issues,
	}

	for _, mx := range dnsResult.MXRecords {
		details.MXRecordsFound = append(details.MXRecordsFound, MXRecordFound{
			Priority: mx.Priority,
			Hostname: mx.Hostname,
			IsValid:  mx.IsValid,
		})
	}

	return details
}

// ToDNSStatus converts DNS check result to DNS status DTO
func ToDNSStatus(dnsResult *domain.DNSCheckResult) DNSStatus {
	status := DNSStatus{
		MXRecords:       make([]MXRecordFound, 0, len(dnsResult.MXRecords)),
		TXTRecords:      make([]TXTRecordFound, 0, len(dnsResult.TXTRecords)),
		MXValid:         dnsResult.MXValid,
		TXTValid:        dnsResult.TXTValid,
		IsReadyToVerify: dnsResult.IsReadyToVerify,
		Issues:          dnsResult.Issues,
	}

	for _, mx := range dnsResult.MXRecords {
		status.MXRecords = append(status.MXRecords, MXRecordFound{
			Priority: mx.Priority,
			Hostname: mx.Hostname,
			IsValid:  mx.IsValid,
		})
	}

	for _, txt := range dnsResult.TXTRecords {
		status.TXTRecords = append(status.TXTRecords, TXTRecordFound{
			Name:    txt.Name,
			Value:   txt.Value,
			IsValid: txt.IsValid,
		})
	}

	return status
}

// CalculateTotalPages calculates total pages for pagination
func CalculateTotalPages(totalCount, perPage int) int {
	if perPage <= 0 {
		return 0
	}
	pages := totalCount / perPage
	if totalCount%perPage > 0 {
		pages++
	}
	return pages
}
