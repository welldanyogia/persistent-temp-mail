/**
 * Realtime (SSE) Event Types
 * Aligned with API Contracts v1.1.0 and Backend Implementation
 */

import { Alias } from './alias';

export type EventType =
  | 'connected'
  | 'heartbeat'
  | 'new_email'
  | 'email_deleted'
  | 'alias_created'
  | 'alias_deleted'
  | 'domain_verified'
  | 'domain_deleted';

export interface RealtimeEvent<T = unknown> {
  type: EventType;
  data: T;
  timestamp: string;
}

// SSE Event: new_email
export interface NewEmailEvent {
  id: string;
  alias_id: string;
  alias_email: string;
  from_address: string;
  from_name?: string; // Optional - some emails don't have display name
  subject?: string;
  preview_text?: string;
  received_at: string;
  has_attachments: boolean;
  attachment_count: number;
  size_bytes: number;
}

// SSE Event: email_deleted
export interface EmailDeletedEvent {
  id: string;
  alias_id: string;
  deleted_at: string;
}

// SSE Event: alias_created
export interface AliasCreatedEvent extends Alias {}

// SSE Event: alias_deleted
export interface AliasDeletedEvent {
  id: string;
  email_address: string;
  deleted_at: string;
  emails_deleted: number;
}

// SSE Event: domain_verified
export interface DomainVerifiedEvent {
  id: string;
  domain_name: string;
  verified_at: string;
  ssl_status: string;
}

// SSE Event: domain_deleted
export interface DomainDeletedEvent {
  id: string;
  domain_name: string;
  deleted_at: string;
  aliases_deleted: number;
  emails_deleted: number;
}

// SSE Event: connected
export interface ConnectedEvent {
  message: string;
  connection_id?: string;
}

// SSE Event: heartbeat
export interface HeartbeatEvent {
  timestamp: string;
}

// Union type for all possible event data
export type SSEEventData =
  | NewEmailEvent
  | EmailDeletedEvent
  | AliasCreatedEvent
  | AliasDeletedEvent
  | DomainVerifiedEvent
  | DomainDeletedEvent
  | ConnectedEvent
  | HeartbeatEvent;
