/**
 * Alias API Service
 * Aligned with API Contracts v1.1.0
 */

import { apiClient } from './client';
import {
  Alias,
  AliasListParams,
  AliasListResponse,
  CreateAliasRequest,
  UpdateAliasRequest,
  DeleteAliasResponse,
} from '@/types/alias';

export const aliasService = {
  /**
   * List aliases with optional filters
   * GET /api/v1/aliases
   */
  list: async (params: AliasListParams = {}): Promise<AliasListResponse> => {
    const query = new URLSearchParams();
    if (params.page) query.append('page', params.page.toString());
    if (params.limit) query.append('limit', params.limit.toString());
    if (params.search) query.append('search', params.search);
    if (params.domain_id) query.append('domain_id', params.domain_id);
    if (params.is_active !== undefined) query.append('is_active', params.is_active.toString());
    if (params.sort) query.append('sort', params.sort);
    if (params.order) query.append('order', params.order);

    return apiClient.get<AliasListResponse>(`/aliases?${query.toString()}`);
  },

  /**
   * Get alias details by ID (includes stats)
   * GET /api/v1/aliases/:id
   */
  get: async (id: string): Promise<Alias> => {
    const response = await apiClient.get<{ alias: Alias }>(`/aliases/${id}`);
    return response.alias;
  },

  /**
   * Create a new alias
   * POST /api/v1/aliases
   */
  create: async (data: CreateAliasRequest): Promise<Alias> => {
    const response = await apiClient.post<{ alias: Alias }>('/aliases', data);
    return response.alias;
  },

  /**
   * Update alias (full update)
   * PUT /api/v1/aliases/:id
   */
  update: async (id: string, data: UpdateAliasRequest): Promise<Alias> => {
    const response = await apiClient.put<{ alias: Alias }>(`/aliases/${id}`, data);
    return response.alias;
  },

  /**
   * Partially update alias
   * PATCH /api/v1/aliases/:id
   */
  patch: async (id: string, data: UpdateAliasRequest): Promise<Alias> => {
    const response = await apiClient.patch<{ alias: Alias }>(`/aliases/${id}`, data);
    return response.alias;
  },

  /**
   * Delete an alias (cascades to emails and attachments)
   * DELETE /api/v1/aliases/:id
   */
  delete: async (id: string): Promise<DeleteAliasResponse> => {
    return apiClient.delete<DeleteAliasResponse>(`/aliases/${id}`);
  },

  /**
   * Toggle alias active status
   * PATCH /api/v1/aliases/:id
   */
  toggle: async (id: string, isActive: boolean): Promise<Alias> => {
    const response = await apiClient.patch<{ alias: Alias }>(`/aliases/${id}`, { is_active: isActive });
    return response.alias;
  },
};
