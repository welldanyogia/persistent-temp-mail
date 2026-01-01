"use client";

import { useState, useEffect } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import * as z from "zod";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Loader2, Plus } from "lucide-react";
import { domainService } from "@/lib/api/domains";
import { Domain } from "@/types/domain";

const createAliasSchema = z.object({
  local_part: z
    .string()
    .min(1, "Username is required")
    .regex(/^[a-z0-9.-]+$/, "Only lowercase letters, numbers, dots, and hyphens are allowed"),
  domain_id: z.string().min(1, "Please select a domain"),
  description: z.string().optional(),
});

type CreateAliasFormValues = z.infer<typeof createAliasSchema>;

interface CreateAliasDialogProps {
  onCreate: (data: { local_part: string; domain_id: string; description?: string }) => Promise<boolean>;
}

export function CreateAliasDialog({ onCreate }: CreateAliasDialogProps) {
  const [open, setOpen] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [domains, setDomains] = useState<Domain[]>([]);
  const [isLoadingDomains, setIsLoadingDomains] = useState(false);

  const form = useForm<CreateAliasFormValues>({
    resolver: zodResolver(createAliasSchema),
    defaultValues: {
      local_part: "",
      domain_id: "",
      description: "",
    },
  });

  // Fetch verified domains when dialog opens
  useEffect(() => {
    if (open) {
      const fetchDomains = async () => {
        setIsLoadingDomains(true);
        try {
          const response = await domainService.list({ status: 'verified' });
          setDomains(response.domains);
          // Set default domain if available and not set
          if (response.domains.length > 0 && !form.getValues("domain_id")) {
            const firstDomainId = response.domains[0]?.id;
            if (firstDomainId) {
              form.setValue("domain_id", firstDomainId);
            }
          }
        } catch (error) {
          console.error("Failed to fetch domains", error);
        } finally {
          setIsLoadingDomains(false);
        }
      };
      fetchDomains();
    }
  }, [open, form]);

  const onSubmit = async (data: CreateAliasFormValues) => {
    setIsSubmitting(true);
    // Filter out undefined description to satisfy exactOptionalPropertyTypes
    const payload: { local_part: string; domain_id: string; description?: string } = {
      local_part: data.local_part,
      domain_id: data.domain_id,
    };
    if (data.description) {
      payload.description = data.description;
    }
    const success = await onCreate(payload);
    setIsSubmitting(false);
    if (success) {
      setOpen(false);
      form.reset();
    }
  };

  const selectedDomainId = form.watch("domain_id");
  const selectedDomain = domains.find((d) => d.id === selectedDomainId);
  const localPart = form.watch("local_part");

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button>
          <Plus className="mr-2 h-4 w-4" />
          Create Alias
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-[425px]">
        <DialogHeader>
          <DialogTitle>Create New Alias</DialogTitle>
          <DialogDescription>
            Create a new temporary email address. You can manage it later.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4 py-4">
          <div className="grid gap-4">
            <div className="grid gap-2">
              <Label htmlFor="domain">Domain</Label>
              <Select
                onValueChange={(value) => form.setValue("domain_id", value)}
                defaultValue={form.getValues("domain_id")}
                value={form.watch("domain_id")}
                disabled={isLoadingDomains}
              >
                <SelectTrigger>
                  <SelectValue placeholder={isLoadingDomains ? "Loading..." : "Select domain"} />
                </SelectTrigger>
                <SelectContent>
                  {domains.map((domain) => (
                    <SelectItem key={domain.id} value={domain.id}>
                      {domain.domain_name}
                    </SelectItem>
                  ))}
                  {domains.length === 0 && !isLoadingDomains && (
                     <SelectItem value="none" disabled>No verified domains found</SelectItem>
                  )}
                </SelectContent>
              </Select>
              {form.formState.errors.domain_id && (
                <p className="text-sm text-destructive">
                  {form.formState.errors.domain_id.message}
                </p>
              )}
            </div>
            
            <div className="grid gap-2">
              <Label htmlFor="local_part">Username</Label>
              <div className="flex items-center gap-2">
                <Input
                  id="local_part"
                  placeholder="custom-name"
                  {...form.register("local_part")}
                  className="flex-1"
                />
                <span className="text-muted-foreground text-sm">
                  @{selectedDomain?.domain_name || "domain.com"}
                </span>
              </div>
              {form.formState.errors.local_part && (
                <p className="text-sm text-destructive">
                  {form.formState.errors.local_part.message}
                </p>
              )}
              {localPart && selectedDomain && (
                <p className="text-xs text-muted-foreground">
                  Full address: <span className="font-medium text-foreground">{localPart}@{selectedDomain.domain_name}</span>
                </p>
              )}
            </div>

            <div className="grid gap-2">
              <Label htmlFor="description">Description (Optional)</Label>
              <Input
                id="description"
                placeholder="For Netflix, Newsletter, etc."
                {...form.register("description")}
              />
            </div>
          </div>
          <DialogFooter>
            <Button type="submit" disabled={isSubmitting || isLoadingDomains || domains.length === 0}>
              {isSubmitting && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Create Alias
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
