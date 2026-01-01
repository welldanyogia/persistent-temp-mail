/**
 * Domain Types
 * Aligned with API Contracts v1.1.0 and Backend Implementation
 */

export type DomainStatus = 'pending' | 'verified';
export type SSLStatus = 'pending' | 'active' | 'expired' | 'failed';

export interface DNSRecord {
  type: string;
  name: string;
  value: string;
  priority?: number;
  status?: 'pending' | 'verified' | 'failed';
}

export interface DNSInstructions {
  mx_record: {
    type: string;
    priority: number;
    value: string;
  };
  txt_record: {
    type: string;
    name: string;
    value: string;
  };
}

export interface Domain {
  id: string;
  domain_name: string;
  status: DomainStatus;
  verification_token?: string; // Only present for pending domains
  mx_record_configured: boolean;
  ssl_status: SSLStatus;
  ssl_expires_at?: string;
  created_at: string;
  updated_at: string;
  verified_at?: string;
  alias_count: number;
  dns_instructions?: DNSInstructions; // Present in creation response
}

export interface CreateDomainRequest {
  domain_name: string;
}

export interface Pagination {
  current_page: number;
  per_page: number;
  total_pages: number;
  total_count: number;
  has_next?: boolean;
  has_previous?: boolean;
}

export interface DomainListResponse {
  domains: Domain[];
  pagination: Pagination;
}

// MX record found during DNS check
export interface MXRecordFound {
  priority: number;
  hostname: string;
  is_valid: boolean;
}

// TXT record found during DNS check
export interface TXTRecordFound {
  name: string;
  value: string;
  is_valid: boolean;
}

// DNS Status from backend (GET /api/v1/domains/:id/dns-status)
export interface DNSStatus {
  mx_records: MXRecordFound[];
  txt_records: TXTRecordFound[];
  mx_valid: boolean;
  txt_valid: boolean;
  is_ready_to_verify: boolean;
  issues?: string[];
}

export interface DNSStatusResponse {
  domain: Domain;
  dns_status: DNSStatus;
}

// Verification details returned after POST /api/v1/domains/:id/verify
export interface VerificationDetails {
  mx_records_found: MXRecordFound[];
  txt_record_found: boolean;
  ssl_certificate_issued: boolean;
  issues?: string[];
}

export interface VerifyDomainResponse {
  domain: Domain;
  verification_details: VerificationDetails;
}

// DELETE /api/v1/domains/:id response
export interface DeleteDomainResponse {
  message: string;
  deleted_resources: {
    domain_id: string;
    aliases_deleted: number;
    emails_deleted: number;
    attachments_deleted: number;
  };
}
