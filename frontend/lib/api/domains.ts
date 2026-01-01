/**
 * Domain API Service
 * Aligned with API Contracts v1.1.0
 */

import { apiClient } from './client';
import {
  Domain,
  DomainListResponse,
  CreateDomainRequest,
  DNSStatusResponse,
  VerifyDomainResponse,
  DeleteDomainResponse,
  DomainStatus,
} from '@/types/domain';

interface DomainListOptions {
  page?: number;
  limit?: number;
  status?: DomainStatus;
}

export const domainService = {
  /**
   * List all domains with optional filtering
   * GET /api/v1/domains
   */
  list: async (options: DomainListOptions = {}): Promise<DomainListResponse> => {
    const query = new URLSearchParams();
    if (options.page) query.append('page', options.page.toString());
    if (options.limit) query.append('limit', options.limit.toString());
    if (options.status) query.append('status', options.status);

    return apiClient.get<DomainListResponse>(`/domains?${query.toString()}`);
  },

  /**
   * Get domain details by ID
   * GET /api/v1/domains/:id
   */
  get: async (id: string): Promise<Domain> => {
    const response = await apiClient.get<{ domain: Domain }>(`/domains/${id}`);
    return response.domain;
  },

  /**
   * Create a new domain
   * POST /api/v1/domains
   */
  create: async (data: CreateDomainRequest): Promise<Domain> => {
    const response = await apiClient.post<{ domain: Domain }>('/domains', data);
    return response.domain;
  },

  /**
   * Delete a domain (cascades to aliases, emails, attachments)
   * DELETE /api/v1/domains/:id
   */
  delete: async (id: string): Promise<DeleteDomainResponse> => {
    return apiClient.delete<DeleteDomainResponse>(`/domains/${id}`);
  },

  /**
   * Trigger DNS verification and SSL certificate generation
   * POST /api/v1/domains/:id/verify
   */
  verify: async (id: string): Promise<VerifyDomainResponse> => {
    return apiClient.post<VerifyDomainResponse>(`/domains/${id}/verify`, {});
  },

  /**
   * Check current DNS configuration status
   * GET /api/v1/domains/:id/dns-status
   */
  getDNSStatus: async (id: string): Promise<DNSStatusResponse> => {
    return apiClient.get<DNSStatusResponse>(`/domains/${id}/dns-status`);
  },
};
