/**
 * Auth API Service
 * Aligned with API Contracts v1.1.0
 */

import { apiClient } from './client';
import {
  AuthData,
  LoginRequest,
  RegisterRequest,
  User,
  DeleteAccountResponse,
} from '@/types/auth';

const API_BASE = process.env.NEXT_PUBLIC_API_URL || 'https://api.webrana.id/api/v1';

export const authService = {
  /**
   * Login with email and password
   * POST /api/v1/auth/login
   */
  login: async (data: LoginRequest): Promise<AuthData> => {
    return apiClient.post<AuthData>('/auth/login', data, { skipAuth: true });
  },

  /**
   * Register new account
   * POST /api/v1/auth/register
   */
  register: async (data: RegisterRequest): Promise<AuthData> => {
    return apiClient.post<AuthData>('/auth/register', data, { skipAuth: true });
  },

  /**
   * Logout and invalidate session
   * POST /api/v1/auth/logout
   * Requires refresh_token in body
   */
  logout: async (): Promise<void> => {
    try {
      const refreshToken = apiClient.getRefreshToken();
      await apiClient.post('/auth/logout', { refresh_token: refreshToken }, { skipAuth: false });
    } finally {
      apiClient.clearTokens();
    }
  },

  /**
   * Get current user profile
   * GET /api/v1/auth/me
   */
  me: async (): Promise<User> => {
    const response = await apiClient.get<{ user: User }>('/auth/me');
    return response.user;
  },

  /**
   * Delete current user account (cascade delete)
   * DELETE /api/v1/auth/me
   */
  deleteAccount: async (): Promise<DeleteAccountResponse> => {
    const response = await apiClient.delete<DeleteAccountResponse>('/auth/me');
    apiClient.clearTokens();
    return response;
  },

  /**
   * Manually refresh access token
   * POST /api/v1/auth/refresh
   * Requires refresh_token in body
   * Note: Refresh is handled automatically by client.ts, but exposed here if needed manually
   */
  refresh: async (): Promise<boolean> => {
    try {
      const refreshToken = apiClient.getRefreshToken();
      if (!refreshToken) {
        return false;
      }

      const response = await fetch(`${API_BASE}/auth/refresh`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ refresh_token: refreshToken }),
      });

      if (response.ok) {
        const data = await response.json();
        const tokens = data.data?.tokens;
        if (tokens?.access_token) {
          apiClient.setToken(tokens.access_token);
          // Update refresh token if new one provided
          if (tokens.refresh_token) {
            apiClient.setRefreshToken(tokens.refresh_token);
          }
          return true;
        }
      }
      return false;
    } catch {
      return false;
    }
  },
};
