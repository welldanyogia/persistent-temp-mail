"use client";

import { useEffect, useRef, useState, useCallback } from "react";
import { toast } from "sonner";
import { 
  NewEmailEvent, 
  EmailDeletedEvent, 
  AliasCreatedEvent, 
  AliasDeletedEvent, 
  DomainVerifiedEvent,
  DomainDeletedEvent
} from "@/types/realtime";

interface UseRealtimeOptions {
  onNewEmail?: (email: NewEmailEvent) => void;
  onEmailDeleted?: (data: EmailDeletedEvent) => void;
  onAliasCreated?: (alias: AliasCreatedEvent) => void;
  onAliasDeleted?: (data: AliasDeletedEvent) => void;
  onDomainVerified?: (domain: DomainVerifiedEvent) => void;
  onDomainDeleted?: (data: DomainDeletedEvent) => void;
  enabled?: boolean;
}

export function useRealtime(options: UseRealtimeOptions = {}) {
  const [connected, setConnected] = useState(false);
  const eventSourceRef = useRef<EventSource | null>(null);
  const reconnectTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const reconnectAttemptsRef = useRef(0);

  const { 
    onNewEmail, 
    onEmailDeleted, 
    onAliasCreated, 
    onAliasDeleted, 
    onDomainVerified,
    onDomainDeleted,
    enabled = true 
  } = options;

  const connect = useCallback(() => {
    if (typeof window === "undefined" || !enabled) return;

    const token = localStorage.getItem("access_token");
    if (!token) return;

    // Close existing connection if any
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
    }

    const API_BASE = process.env.NEXT_PUBLIC_API_URL || "https://api.webrana.id/v1";
    // Ensure we don't have double /v1 if API_BASE already has it
    const url = `${API_BASE}/events?token=${token}`;
    
    const eventSource = new EventSource(url);

    eventSource.onopen = () => {
      console.log("SSE connection established");
      setConnected(true);
      reconnectAttemptsRef.current = 0;
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
      }
    };

    // Generic message handler if data is sent without specific event name
    // But our backend uses event: name format
    
    eventSource.addEventListener("connected", (event: MessageEvent) => {
      console.log("SSE Connected Event:", JSON.parse(event.data));
    });

    eventSource.addEventListener("new_email", (event: MessageEvent) => {
      try {
        const data: NewEmailEvent = JSON.parse(event.data);
        onNewEmail?.(data);
        toast.info(`New email from ${data.from_name || data.from_address}`, {
          description: data.subject,
        });
      } catch (err) {
        console.error("Failed to parse new_email event", err);
      }
    });

    eventSource.addEventListener("email_deleted", (event: MessageEvent) => {
      try {
        const data: EmailDeletedEvent = JSON.parse(event.data);
        onEmailDeleted?.(data);
      } catch (err) {
        console.error("Failed to parse email_deleted event", err);
      }
    });

    eventSource.addEventListener("alias_created", (event: MessageEvent) => {
      try {
        const data: AliasCreatedEvent = JSON.parse(event.data);
        onAliasCreated?.(data);
      } catch (err) {
        console.error("Failed to parse alias_created event", err);
      }
    });

    eventSource.addEventListener("alias_deleted", (event: MessageEvent) => {
      try {
        const data: AliasDeletedEvent = JSON.parse(event.data);
        onAliasDeleted?.(data);
      } catch (err) {
        console.error("Failed to parse alias_deleted event", err);
      }
    });

    eventSource.addEventListener("domain_verified", (event: MessageEvent) => {
      try {
        const data: DomainVerifiedEvent = JSON.parse(event.data);
        onDomainVerified?.(data);
        toast.success(`Domain ${data.domain_name} verified successfully!`);
      } catch (err) {
        console.error("Failed to parse domain_verified event", err);
      }
    });

    eventSource.addEventListener("domain_deleted", (event: MessageEvent) => {
      try {
        const data: DomainDeletedEvent = JSON.parse(event.data);
        onDomainDeleted?.(data);
      } catch (err) {
        console.error("Failed to parse domain_deleted event", err);
      }
    });

    eventSource.addEventListener("heartbeat", () => {
      // Just keep-alive
    });

    eventSource.onerror = (err) => {
      console.error("SSE Error:", err);
      setConnected(false);
      eventSource.close();

      // Exponential backoff
      const delay = Math.min(1000 * Math.pow(2, reconnectAttemptsRef.current), 30000);
      reconnectAttemptsRef.current++;

      console.log(`SSE reconnecting in ${delay}ms... (attempt ${reconnectAttemptsRef.current})`);
      
      reconnectTimeoutRef.current = setTimeout(() => {
        connect();
      }, delay);
    };

    eventSourceRef.current = eventSource;
  }, [enabled, onNewEmail, onEmailDeleted, onAliasCreated, onAliasDeleted, onDomainVerified, onDomainDeleted]);

  useEffect(() => {
    if (enabled) {
      connect();
    }

    return () => {
      if (eventSourceRef.current) {
        eventSourceRef.current.close();
      }
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
      }
    };
  }, [connect, enabled]);

  return { connected };
}
