"use client";

import { useState, useEffect, useCallback } from "react";
import { emailService } from "@/lib/api/emails";
import { EmailListItem, EmailListParams } from "@/types/email";
import { toast } from "sonner";

interface UseEmailsOptions extends Omit<EmailListParams, 'page'> {
  autoFetch?: boolean;
  initialPage?: number;
}

export function useEmails(options: UseEmailsOptions = {}) {
  const [emails, setEmails] = useState<EmailListItem[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(options.initialPage || 1);
  const [hasMore, setHasMore] = useState(false);
  
  // Selection state
  const [selectedIds, setSelectedIds] = useState<string[]>([]);

  // Internal state for filters to avoid excessive re-renders
  const [params, setParams] = useState<EmailListParams>({
    limit: options.limit || 20,
    search: options.search || "",
    alias_id: options.alias_id || "",
    from_date: options.from_date || "",
    to_date: options.to_date || "",
    ...options
  });

  const fetchEmails = useCallback(async (currentParams: EmailListParams, currentPage: number) => {
    setIsLoading(true);
    try {
      const response = await emailService.list({
        ...currentParams,
        page: currentPage,
      });
      
      if (currentPage === 1) {
        setEmails(response.emails);
      } else {
        setEmails((prev) => [...prev, ...response.emails]);
      }
      
      setTotal(response.pagination.total_count);
      setHasMore(currentPage < response.pagination.total_pages);
      setError(null);
    } catch (err: unknown) {
      if (err instanceof Error) {
        setError(err);
      }
      const msg = err instanceof Error ? err.message : "Failed to fetch emails";
      toast.error(msg);
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    if (options.autoFetch !== false) {
      fetchEmails(params, page);
    }
  }, [params, page, fetchEmails, options.autoFetch]);

  // Real-time event listeners
  useEffect(() => {
    const handleNewEmail = (event: Event) => {
      const emailData = (event as CustomEvent).detail;
      
      // Check if new email matches current alias filter
      if (params.alias_id && emailData.alias_id !== params.alias_id) {
        return;
      }

      // Check if matches search (simple check)
      if (params.search && !emailData.subject.toLowerCase().includes(params.search.toLowerCase())) {
        return;
      }

      const newEmail: EmailListItem = {
        ...emailData,
        is_read: false,
        attachment_count: emailData.has_attachments ? 1 : 0, // Fallback if not provided
        content_type: "text/html", // Default
      };

      setEmails((prev) => {
        // Prevent duplicates
        if (prev.some(e => e.id === newEmail.id)) return prev;
        return [newEmail, ...prev];
      });
      setTotal((prev) => prev + 1);
    };

    const handleEmailDeleted = (event: Event) => {
      const { id } = (event as CustomEvent).detail;
      setEmails((prev) => prev.filter(e => e.id !== id));
      setTotal((prev) => Math.max(0, prev - 1));
    };

    window.addEventListener("realtime:new_email", handleNewEmail);
    window.addEventListener("realtime:email_deleted", handleEmailDeleted);

    return () => {
      window.removeEventListener("realtime:new_email", handleNewEmail);
      window.removeEventListener("realtime:email_deleted", handleEmailDeleted);
    };
  }, [params.alias_id, params.search]);

  const loadMore = () => {
    if (hasMore && !isLoading) {
      setPage((prevPage) => prevPage + 1);
    }
  };

  const refresh = () => {
    setPage(1);
    fetchEmails(params, 1);
  };

  const updateFilters = (newParams: Partial<EmailListParams>) => {
    setParams((prev) => ({ ...prev, ...newParams }));
    setPage(1); // Reset to first page on filter change
  };

  // Bulk Actions
  const deleteEmails = async (ids: string[]) => {
    if (ids.length === 0) return;
    try {
      if (ids.length === 1) {
        const id = ids[0];
        if (id) await emailService.delete(id);
      } else {
        await emailService.bulkDelete(ids);
      }
      
      // Optimistic update
      setEmails((prev) => prev.filter((email) => !ids.includes(email.id)));
      setSelectedIds((prev) => prev.filter((id) => !ids.includes(id)));
      setTotal((prev) => prev - ids.length);
      
      toast.success(`${ids.length} email(s) deleted`);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Failed to delete emails";
      toast.error(msg);
    }
  };

  const markAsRead = async (ids: string[]) => {
    if (ids.length === 0) return;
    try {
      if (ids.length === 1) {
        const id = ids[0];
        if (id) await emailService.markAsRead(id);
      } else {
        await emailService.bulkMarkAsRead(ids);
      }

      // Optimistic update
      setEmails((prev) =>
        prev.map((email) =>
          ids.includes(email.id) ? { ...email, is_read: true } : email
        )
      );
      
      toast.success(`${ids.length} email(s) marked as read`);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Failed to update emails";
      toast.error(msg);
    }
  };

  const toggleSelection = (id: string) => {
    setSelectedIds((prev) =>
      prev.includes(id) ? prev.filter((i) => i !== id) : [...prev, id]
    );
  };

  const selectAll = () => {
    if (selectedIds.length === emails.length) {
      setSelectedIds([]);
    } else {
      setSelectedIds(emails.map((e) => e.id));
    }
  };

  return {
    emails,
    isLoading,
    error,
    total,
    page,
    hasMore,
    loadMore,
    refresh,
    updateFilters,
    filters: params,
    // Selection
    selectedIds,
    toggleSelection,
    selectAll,
    // Actions
    deleteEmails,
    markAsRead,
  };
}
