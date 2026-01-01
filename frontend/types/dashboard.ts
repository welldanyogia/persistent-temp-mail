/**
 * Dashboard Types
 * Aligned with API Contracts v1.1.0 and Backend Implementation
 */

export interface DashboardStats {
  total_domains: number;
  total_aliases: number;
  total_emails: number;
  unread_emails: number;
  storage_used_bytes: number;
}

// Extended inbox stats (from backend InboxStats)
export interface InboxStats {
  total_emails: number;
  unread_emails: number;
  total_size_bytes: number;
  emails_today: number;
  emails_this_week: number;
  emails_this_month: number;
  emails_per_alias: AliasEmailCount[];
}

export interface AliasEmailCount {
  alias_id: string;
  alias_email: string;
  count: number;
}

// Recent email for dashboard display
export interface RecentEmail {
  id: string;
  alias_email: string;
  from_address: string;
  from_name?: string;
  subject?: string;
  preview_text?: string;
  received_at: string;
  is_read: boolean;
  has_attachments: boolean;
}

// Quick action types for dashboard
export interface QuickAction {
  id: string;
  label: string;
  icon: string;
  href?: string;
  onClick?: () => void;
}
