/**
 * Alias Types
 * Aligned with API Contracts v1.1.0 and Backend Implementation
 */

import { Pagination } from './domain';

export interface TopSender {
  email: string;
  count: number;
}

export interface AliasStats {
  emails_today: number;
  emails_this_week: number;
  emails_this_month: number;
  top_senders: TopSender[];
}

export interface Alias {
  id: string;
  email_address: string;
  local_part: string;
  domain_id: string;
  domain_name: string;
  description?: string;
  is_active: boolean;
  created_at: string;
  updated_at?: string;
  email_count: number;
  last_email_received_at?: string;
  total_size_bytes: number;
  stats?: AliasStats; // Only present in detail response
}

export interface CreateAliasRequest {
  local_part: string;
  domain_id: string;
  description?: string;
}

export interface UpdateAliasRequest {
  is_active?: boolean;
  description?: string;
}

export interface AliasListParams {
  page?: number;
  limit?: number;
  domain_id?: string;
  search?: string;
  is_active?: boolean;
  sort?: 'created_at' | 'email_count' | 'last_email_received_at';
  order?: 'asc' | 'desc';
}

export interface AliasListResponse {
  aliases: Alias[];
  pagination: Pagination;
}

// DELETE /api/v1/aliases/:id response
export interface DeleteAliasResponse {
  message: string;
  deleted_resources: {
    alias_id: string;
    emails_deleted: number;
    attachments_deleted: number;
  };
}
