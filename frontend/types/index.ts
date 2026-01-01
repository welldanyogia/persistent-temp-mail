/**
 * Types Index
 * Central export point for all TypeScript types
 * Aligned with API Contracts v1.1.0
 */

// Auth types
export type {
  User,
  LoginRequest,
  RegisterRequest,
  Tokens,
  AuthData,
  AuthResponse,
  DeleteAccountResponse,
  RefreshResponse,
} from './auth';

// Domain types
export type {
  DomainStatus,
  SSLStatus,
  DNSRecord,
  DNSInstructions,
  Domain,
  CreateDomainRequest,
  Pagination,
  DomainListResponse,
  MXRecordFound,
  TXTRecordFound,
  DNSStatus,
  DNSStatusResponse,
  VerificationDetails,
  VerifyDomainResponse,
  DeleteDomainResponse,
} from './domain';

// Alias types
export type {
  TopSender,
  AliasStats,
  Alias,
  CreateAliasRequest,
  UpdateAliasRequest,
  AliasListParams,
  AliasListResponse,
  DeleteAliasResponse,
} from './alias';

// Email types
export type {
  Attachment,
  EmailHeaders,
  Email,
  EmailListItem,
  EmailListParams,
  EmailListResponse,
  BulkOperationResult,
  BulkOperationResponse,
  BulkDeleteRequest,
  BulkMarkReadRequest,
  DeleteEmailResponse,
  PreSignedURLResponse,
  MarkAsReadResponse,
} from './email';

// Realtime types
export type {
  EventType,
  RealtimeEvent,
  NewEmailEvent,
  EmailDeletedEvent,
  AliasCreatedEvent,
  AliasDeletedEvent,
  DomainVerifiedEvent,
  DomainDeletedEvent,
  ConnectedEvent,
  HeartbeatEvent,
  SSEEventData,
} from './realtime';

// Dashboard types
export type {
  DashboardStats,
  InboxStats,
  AliasEmailCount,
  RecentEmail,
  QuickAction,
} from './dashboard';

// API Response wrapper type (generic)
export interface ApiResponse<T = unknown> {
  success: boolean;
  data?: T;
  error?: {
    code: string;
    message: string;
    details?: Record<string, string[]>;
  };
  timestamp: string;
}

// Error codes enum for type-safe error handling
export enum ErrorCode {
  // Auth errors
  VALIDATION_ERROR = 'VALIDATION_ERROR',
  EMAIL_EXISTS = 'EMAIL_EXISTS',
  INVALID_CREDENTIALS = 'INVALID_CREDENTIALS',
  TOO_MANY_ATTEMPTS = 'TOO_MANY_ATTEMPTS',
  AUTH_TOKEN_MISSING = 'AUTH_TOKEN_MISSING',
  AUTH_TOKEN_INVALID = 'AUTH_TOKEN_INVALID',
  INVALID_REFRESH_TOKEN = 'INVALID_REFRESH_TOKEN',

  // Resource errors
  RESOURCE_NOT_FOUND = 'RESOURCE_NOT_FOUND',
  RESOURCE_ACCESS_DENIED = 'RESOURCE_ACCESS_DENIED',
  DOMAIN_NOT_FOUND = 'DOMAIN_NOT_FOUND',
  ALIAS_NOT_FOUND = 'ALIAS_NOT_FOUND',
  EMAIL_NOT_FOUND = 'EMAIL_NOT_FOUND',
  ATTACHMENT_NOT_FOUND = 'ATTACHMENT_NOT_FOUND',
  ATTACHMENT_DELETED = 'ATTACHMENT_DELETED',

  // Validation errors
  DOMAIN_ALREADY_EXISTS = 'DOMAIN_ALREADY_EXISTS',
  ALIAS_ALREADY_EXISTS = 'ALIAS_ALREADY_EXISTS',
  DOMAIN_NOT_VERIFIED = 'DOMAIN_NOT_VERIFIED',
  VERIFICATION_FAILED = 'VERIFICATION_FAILED',

  // Rate limiting
  RATE_LIMIT_EXCEEDED = 'RATE_LIMIT_EXCEEDED',
  BULK_LIMIT_EXCEEDED = 'BULK_LIMIT_EXCEEDED',

  // Server errors
  INTERNAL_ERROR = 'INTERNAL_ERROR',
  SSL_GENERATION_IN_PROGRESS = 'SSL_GENERATION_IN_PROGRESS',
}
