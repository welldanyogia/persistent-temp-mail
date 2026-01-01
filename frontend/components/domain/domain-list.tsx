"use client";

import { useState, useEffect, useCallback } from "react";
import { domainService } from "@/lib/api/domains";
import { Domain } from "@/types/domain";
import { DomainCard } from "./domain-card";
import { AddDomainDialog } from "./add-domain-dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "sonner";
import { Globe } from "lucide-react";

export function DomainList() {
  const [domains, setDomains] = useState<Domain[]>([]);
  const [isLoading, setIsLoading] = useState(true);

  const fetchDomains = useCallback(async () => {
    setIsLoading(true);
    try {
      const response = await domainService.list({ limit: 100 });
      setDomains(response.domains);
    } catch (err: unknown) {
      console.error("Failed to fetch domains", err);
      toast.error("Failed to load domains");
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchDomains();
  }, [fetchDomains]);

  // Real-time event listener
  useEffect(() => {
    const handleVerified = (event: Event) => {
      const verifiedDomain = (event as CustomEvent).detail;
      setDomains((prev) => 
        prev.map(d => d.id === verifiedDomain.id ? { ...d, status: 'verified', ssl_status: verifiedDomain.ssl_status } : d)
      );
    };

    window.addEventListener("realtime:domain_verified", handleVerified);
    return () => window.removeEventListener("realtime:domain_verified", handleVerified);
  }, []);

  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row gap-4 justify-between items-start sm:items-center">
        <div>
          <h2 className="text-2xl font-bold tracking-tight">Domains</h2>
          <p className="text-muted-foreground">
            Manage your custom domains and DNS configurations.
          </p>
        </div>
        <AddDomainDialog onDomainAdded={fetchDomains} />
      </div>

      {isLoading ? (
        <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="border rounded-lg p-4 space-y-4">
              <div className="flex justify-between items-start">
                <div className="space-y-2">
                  <Skeleton className="h-5 w-40" />
                  <Skeleton className="h-4 w-20" />
                </div>
                <Skeleton className="h-8 w-24" />
              </div>
              <Skeleton className="h-4 w-full" />
              <div className="pt-4 flex justify-between">
                <Skeleton className="h-4 w-16" />
                <Skeleton className="h-8 w-8" />
              </div>
            </div>
          ))}
        </div>
      ) : domains.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-center border rounded-lg bg-muted/5 border-dashed">
          <div className="h-16 w-16 rounded-full bg-muted flex items-center justify-center mb-4">
            <Globe className="h-8 w-8 text-muted-foreground" />
          </div>
          <h3 className="text-xl font-medium mb-2">No custom domains</h3>
          <p className="text-sm text-muted-foreground max-w-sm mx-auto mb-6">
            Add a custom domain to create personalized email aliases like 
            <span className="font-mono bg-muted px-1 py-0.5 rounded mx-1">name@yourdomain.com</span>
          </p>
          <AddDomainDialog onDomainAdded={fetchDomains} />
        </div>
      ) : (
        <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
          {domains.map((domain) => (
            <DomainCard
              key={domain.id}
              domain={domain}
              onUpdate={fetchDomains}
            />
          ))}
        </div>
      )}
    </div>
  );
}