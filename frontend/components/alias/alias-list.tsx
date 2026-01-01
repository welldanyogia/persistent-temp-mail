"use client";

import { useAliases } from "@/hooks/use-aliases";
import { AliasCard } from "./alias-card";
import { CreateAliasDialog } from "./create-alias-dialog";
import { Input } from "@/components/ui/input";
import { Search, ShieldAlert } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";

export function AliasList() {
  const { 
    aliases, 
    isLoading, 
    search, 
    setSearch, 
    createAlias, 
    toggleAlias, 
    deleteAlias,
    refresh,
  } = useAliases();

  const handleCreateAlias = async (data: { local_part: string; domain_id: string; description?: string }) => {
    // The API contract for creation only accepts local_part and domain_id.
    // Description might need to be set via update later or if the API supports it despite the contract.
    // For now, we strictly follow the contract type.
    const { local_part, domain_id } = data;
    return createAlias({ local_part, domain_id });
  };

  const handleGenerate = () => {
    refresh();
  };

  const filteredAliases = aliases.filter(alias => 
    alias.email_address.toLowerCase().includes(search.toLowerCase()) ||
    alias.description?.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row gap-4 justify-between items-start sm:items-center">
        <div className="relative w-full max-w-sm">
          <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search aliases..."
            className="pl-8"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>
        <CreateAliasDialog onCreate={handleCreateAlias} onGenerate={handleGenerate} />
      </div>

      {isLoading ? (
        <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="border rounded-lg p-4 space-y-3">
              <div className="flex justify-between">
                <Skeleton className="h-5 w-40" />
                <Skeleton className="h-5 w-10" />
              </div>
              <Skeleton className="h-4 w-20" />
              <div className="pt-4 flex justify-between">
                <Skeleton className="h-4 w-16" />
                <Skeleton className="h-8 w-20" />
              </div>
            </div>
          ))}
        </div>
      ) : filteredAliases.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-12 text-center border rounded-lg bg-muted/10 border-dashed">
          <div className="h-12 w-12 rounded-full bg-muted flex items-center justify-center mb-4">
            <ShieldAlert className="h-6 w-6 text-muted-foreground" />
          </div>
          <h3 className="text-lg font-medium">No aliases found</h3>
          <p className="text-sm text-muted-foreground max-w-xs mx-auto mt-2 mb-6">
            {search 
              ? "Try adjusting your search terms." 
              : "Create your first alias to start receiving emails."}
          </p>
          {!search && (
            <CreateAliasDialog onCreate={handleCreateAlias} onGenerate={handleGenerate} />
          )}
        </div>
      ) : (
        <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
          {filteredAliases.map((alias) => (
            <AliasCard
              key={alias.id}
              alias={alias}
              onToggle={toggleAlias}
              onDelete={deleteAlias}
            />
          ))}
        </div>
      )}
    </div>
  );
}
