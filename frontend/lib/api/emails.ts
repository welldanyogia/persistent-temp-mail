/**
 * Email API Service
 * Aligned with API Contracts v1.1.0
 */

import { apiClient } from './client';
import {
  Email,
  EmailListParams,
  EmailListResponse,
  BulkOperationResponse,
  DeleteEmailResponse,
  PreSignedURLResponse,
  MarkAsReadResponse,
} from '@/types/email';

export const emailService = {
  /**
   * List emails with optional filters
   * GET /api/v1/emails
   */
  list: async (params: EmailListParams = {}): Promise<EmailListResponse> => {
    const query = new URLSearchParams();
    if (params.page) query.append('page', params.page.toString());
    if (params.limit) query.append('limit', params.limit.toString());
    if (params.search) query.append('search', params.search);
    if (params.alias_id) query.append('alias_id', params.alias_id);
    if (params.from_date) query.append('from_date', params.from_date);
    if (params.to_date) query.append('to_date', params.to_date);
    if (params.has_attachments !== undefined) query.append('has_attachments', params.has_attachments.toString());
    if (params.is_read !== undefined) query.append('is_read', params.is_read.toString());
    if (params.sort) query.append('sort', params.sort);
    if (params.order) query.append('order', params.order);

    return apiClient.get<EmailListResponse>(`/emails?${query.toString()}`);
  },

  /**
   * Get email details by ID
   * GET /api/v1/emails/:id
   */
  get: async (id: string): Promise<Email> => {
    const response = await apiClient.get<{ email: Email }>(`/emails/${id}`);
    return response.email;
  },

  /**
   * Delete single email
   * DELETE /api/v1/emails/:id
   */
  delete: async (id: string): Promise<DeleteEmailResponse> => {
    return apiClient.delete<DeleteEmailResponse>(`/emails/${id}`);
  },

  /**
   * Bulk delete emails (max 100)
   * POST /api/v1/emails/bulk/delete
   */
  bulkDelete: async (email_ids: string[]): Promise<BulkOperationResponse> => {
    return apiClient.post<BulkOperationResponse>('/emails/bulk/delete', { email_ids });
  },

  /**
   * Mark single email as read
   * PATCH /api/v1/emails/:id/read
   */
  markAsRead: async (id: string, is_read: boolean = true): Promise<MarkAsReadResponse> => {
    return apiClient.patch<MarkAsReadResponse>(`/emails/${id}/read`, { is_read });
  },

  /**
   * Bulk mark emails as read/unread (max 100)
   * POST /api/v1/emails/bulk/mark-read
   */
  bulkMarkAsRead: async (email_ids: string[], is_read: boolean = true): Promise<BulkOperationResponse> => {
    return apiClient.post<BulkOperationResponse>('/emails/bulk/mark-read', { email_ids, is_read });
  },

  /**
   * Get pre-signed URL for attachment download
   * GET /api/v1/emails/:id/attachments/:attachmentId/url
   */
  getAttachmentUrl: async (emailId: string, attachmentId: string): Promise<PreSignedURLResponse> => {
    return apiClient.get<PreSignedURLResponse>(`/emails/${emailId}/attachments/${attachmentId}/url`);
  },

  /**
   * Get attachment download URL (direct, streams file)
   * GET /api/v1/emails/:id/attachments/:attachmentId
   */
  getAttachmentDownloadUrl: (emailId: string, attachmentId: string, inline: boolean = false): string => {
    const base = process.env.NEXT_PUBLIC_API_URL || 'https://api.webrana.id/v1';
    return `${base}/emails/${emailId}/attachments/${attachmentId}${inline ? '?inline=true' : ''}`;
  },
};
