/**
 * Authentication Types
 * Aligned with API Contracts v1.1.0 and Backend Implementation
 */

export interface User {
  id: string;
  email: string;
  created_at: string;
  updated_at?: string;
  last_login?: string; // Optional - null for new users
  is_active?: boolean; // Optional - defaults to true
  domain_count: number;
  alias_count: number;
  email_count: number;
}

export interface LoginRequest {
  email: string;
  password: string;
}

export interface RegisterRequest {
  email: string;
  password: string;
  confirm_password: string;
}

export interface Tokens {
  access_token: string;
  refresh_token: string;
  expires_in: number;
  token_type: string;
}

export interface AuthData {
  user: User;
  tokens: Tokens;
}

export interface AuthResponse {
  success: boolean;
  data?: AuthData;
  error?: {
    code: string;
    message: string;
    details?: Record<string, string[]>;
  };
  timestamp: string;
}

// DELETE /api/v1/auth/me response
export interface DeleteAccountResponse {
  message: string;
  deleted_resources: {
    user_id: string;
    domains_deleted: number;
    aliases_deleted: number;
    emails_deleted: number;
    attachments_deleted: number;
  };
}

// Refresh token response (when manually refreshing)
export interface RefreshResponse {
  tokens: Tokens;
}
