/**
 * Email Types
 * Aligned with API Contracts v1.1.0 and Backend Implementation
 */

import { Pagination } from './domain';

export interface Attachment {
  id: string;
  filename: string;
  content_type: string;
  size_bytes: number;
  download_url: string;
  created_at: string;
}

export interface EmailHeaders {
  [key: string]: string;
}

// Full email detail (GET /api/v1/emails/:id)
export interface Email {
  id: string;
  alias_id: string;
  alias_email: string;
  from_address: string;
  from_name?: string;
  subject?: string;
  body_html?: string;
  body_text?: string;
  preview_text?: string;
  headers?: EmailHeaders;
  received_at: string;
  size_bytes: number;
  is_read: boolean;
  read_at?: string;
  has_attachments: boolean;
  attachment_count: number;
  attachments: Attachment[];
  content_type: string;
  spam_score?: number;
  is_spam: boolean;
  // Extended fields from spec
  cc?: string[];
  bcc?: string[];
  reply_to?: string;
}

// Email list item (GET /api/v1/emails - array items)
export interface EmailListItem {
  id: string;
  alias_id: string;
  alias_email: string;
  from_address: string;
  from_name?: string;
  subject?: string;
  preview_text?: string;
  received_at: string;
  has_attachments: boolean;
  attachment_count: number;
  size_bytes: number;
  is_read: boolean;
  content_type: string;
}

export interface EmailListParams {
  page?: number;
  limit?: number;
  alias_id?: string;
  search?: string;
  from_date?: string;
  to_date?: string;
  has_attachments?: boolean;
  is_read?: boolean; // Filter by read status
  sort?: 'received_at' | 'size';
  order?: 'asc' | 'desc';
}

export interface EmailListResponse {
  emails: EmailListItem[];
  pagination: Pagination;
}

// Bulk operation response (POST /api/v1/emails/bulk/delete, /bulk/mark-read)
export interface BulkOperationResult {
  requested: number;
  deleted?: number;
  updated?: number;
  failed: number;
  failed_ids?: string[];
  attachments_deleted?: number;
  total_size_freed_bytes?: number;
}

export interface BulkOperationResponse {
  message: string;
  operation_results: BulkOperationResult;
}

// Bulk request bodies
export interface BulkDeleteRequest {
  email_ids: string[];
}

export interface BulkMarkReadRequest {
  email_ids: string[];
  is_read: boolean;
}

// Delete single email response
export interface DeleteEmailResponse {
  message: string;
  deleted_resources: {
    email_id: string;
    attachments_deleted: number;
    total_size_freed_bytes: number;
  };
}

// Pre-signed URL response (GET /api/v1/emails/:id/attachments/:aid/url)
export interface PreSignedURLResponse {
  download_url: string;
  expires_at: string;
  filename: string;
  content_type: string;
  size_bytes: number;
}

// Mark as read response
export interface MarkAsReadResponse {
  email: {
    id: string;
    is_read: boolean;
    read_at: string;
  };
}
