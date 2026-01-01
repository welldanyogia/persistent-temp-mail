"use client";

import { useState, useEffect, useCallback } from "react";
import { aliasService } from "@/lib/api/aliases";
import { Alias, CreateAliasRequest } from "@/types/alias";
import { toast } from "sonner";

export function useAliases() {
  const [aliases, setAliases] = useState<Alias[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [search, setSearch] = useState("");

  const fetchAliases = useCallback(async () => {
    setIsLoading(true);
    try {
      const response = await aliasService.list({ 
        limit: 100,
        search: search 
      });
      setAliases(response.aliases);
    } catch (err) {
      console.error("Failed to fetch aliases", err);
      toast.error("Failed to load aliases");
    } finally {
      setIsLoading(false);
    }
  }, [search]);

  useEffect(() => {
    fetchAliases();
  }, [fetchAliases]);

  // Real-time event listeners
  useEffect(() => {
    const handleCreated = (event: Event) => {
      const aliasData = (event as CustomEvent).detail;
      
      const [local_part, domain_name] = aliasData.email_address.split("@");

      const newAlias: Alias = {
        ...aliasData,
        local_part: local_part || "",
        domain_name: domain_name || "",
        email_count: 0,
        total_size_bytes: 0,
        is_active: true,
      };

      setAliases((prev) => {
        if (prev.some(a => a.id === newAlias.id)) return prev;
        return [newAlias, ...prev];
      });
    };

    const handleDeleted = (event: Event) => {
      const { id } = (event as CustomEvent).detail;
      setAliases((prev) => prev.filter(a => a.id !== id));
    };

    window.addEventListener("realtime:alias_created", handleCreated);
    window.addEventListener("realtime:alias_deleted", handleDeleted);

    return () => {
      window.removeEventListener("realtime:alias_created", handleCreated);
      window.removeEventListener("realtime:alias_deleted", handleDeleted);
    };
  }, []);

  const createAlias = async (data: CreateAliasRequest) => {
    try {
      await aliasService.create(data);
      toast.success("Alias created successfully");
      fetchAliases();
      return true;
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Failed to create alias";
      toast.error(msg);
      return false;
    }
  };

  const toggleAlias = async (id: string, currentStatus: boolean) => {
    try {
      // Optimistic update
      setAliases(prev => prev.map(a => a.id === id ? { ...a, is_active: !currentStatus } : a));
      
      await aliasService.toggle(id, !currentStatus);
      toast.success(`Alias ${!currentStatus ? 'enabled' : 'disabled'}`);
    } catch (err: unknown) {
      // Revert on error
      setAliases(prev => prev.map(a => a.id === id ? { ...a, is_active: currentStatus } : a));
      const msg = err instanceof Error ? err.message : "Failed to update alias status";
      toast.error(msg);
    }
  };

  const deleteAlias = async (id: string) => {
    try {
      await aliasService.delete(id);
      setAliases(prev => prev.filter(a => a.id !== id));
      toast.success("Alias deleted successfully");
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Failed to delete alias";
      toast.error(msg);
    }
  };

  return { 
    aliases, 
    isLoading, 
    search, 
    setSearch, 
    createAlias, 
    toggleAlias, 
    deleteAlias,
    refresh: fetchAliases 
  };
}
