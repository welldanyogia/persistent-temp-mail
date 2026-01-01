"use client";

import { createContext, useContext, useCallback, ReactNode } from "react";
import { useRealtime } from "@/hooks/use-realtime";
import { 
  NewEmailEvent, 
  EmailDeletedEvent, 
  AliasCreatedEvent, 
  AliasDeletedEvent, 
  DomainVerifiedEvent
} from "@/types/realtime";
import { useAuth } from "./auth-context";

interface RealtimeContextType {
  connected: boolean;
}

const RealtimeContext = createContext<RealtimeContextType | undefined>(undefined);

export function RealtimeProvider({ children }: { children: ReactNode }) {
  const { isAuthenticated } = useAuth();
  
  // State to notify components of events if they don't want to use listeners
  // But usually components will listen to events themselves or we use global state
  // For now, we'll just handle global side effects here if needed.

  const handleNewEmail = useCallback((email: NewEmailEvent) => {
    // Global handling if needed (e.g. updating unread count in some global store)
    // Actually, use-emails hook might want to know about this.
    // We can use a custom event or a shared state.
    // For now, the toast is already in use-realtime hook.
    window.dispatchEvent(new CustomEvent("realtime:new_email", { detail: email }));
  }, []);

  const handleEmailDeleted = useCallback((data: EmailDeletedEvent) => {
    window.dispatchEvent(new CustomEvent("realtime:email_deleted", { detail: data }));
  }, []);

  const handleAliasCreated = useCallback((alias: AliasCreatedEvent) => {
    window.dispatchEvent(new CustomEvent("realtime:alias_created", { detail: alias }));
  }, []);

  const handleAliasDeleted = useCallback((data: AliasDeletedEvent) => {
    window.dispatchEvent(new CustomEvent("realtime:alias_deleted", { detail: data }));
  }, []);

  const handleDomainVerified = useCallback((domain: DomainVerifiedEvent) => {
    window.dispatchEvent(new CustomEvent("realtime:domain_verified", { detail: domain }));
  }, []);

  const { connected } = useRealtime({
    enabled: isAuthenticated,
    onNewEmail: handleNewEmail,
    onEmailDeleted: handleEmailDeleted,
    onAliasCreated: handleAliasCreated,
    onAliasDeleted: handleAliasDeleted,
    onDomainVerified: handleDomainVerified,
  });

  return (
    <RealtimeContext.Provider value={{ connected }}>
      {children}
    </RealtimeContext.Provider>
  );
}

export function useRealtimeStatus() {
  const context = useContext(RealtimeContext);
  if (context === undefined) {
    throw new Error("useRealtimeStatus must be used within a RealtimeProvider");
  }
  return context;
}
