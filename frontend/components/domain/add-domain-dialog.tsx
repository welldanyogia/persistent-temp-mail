"use client";

import { useState } from "react";
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
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Loader2, Plus } from "lucide-react";
import { domainService } from "@/lib/api/domains";
import { Domain } from "@/types/domain";
import { toast } from "sonner";
import { DNSInstructions } from "./dns-instructions";

const createDomainSchema = z.object({
  domain_name: z
    .string()
    .min(3, "Domain name is required")
    .regex(/^([a-z0-9]+(-[a-z0-9]+)*\.)+[a-z]{2,}$/i, "Invalid domain format (e.g., example.com)"),
});

type CreateDomainFormValues = z.infer<typeof createDomainSchema>;

interface AddDomainDialogProps {
  onDomainAdded: () => void;
}

export function AddDomainDialog({ onDomainAdded }: AddDomainDialogProps) {
  const [open, setOpen] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [createdDomain, setCreatedDomain] = useState<Domain | null>(null);

  const form = useForm<CreateDomainFormValues>({
    resolver: zodResolver(createDomainSchema),
    defaultValues: {
      domain_name: "",
    },
  });

  const onSubmit = async (data: CreateDomainFormValues) => {
    setIsSubmitting(true);
    try {
      const domain = await domainService.create(data);
      setCreatedDomain(domain);
      toast.success("Domain added successfully");
      onDomainAdded();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Failed to add domain";
      toast.error(msg);
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleClose = (isOpen: boolean) => {
    // Prevent closing if submitting
    if (isSubmitting && !isOpen) return;
    
    setOpen(isOpen);
    if (!isOpen) {
      // Reset form after transition
      setTimeout(() => {
        setCreatedDomain(null);
        form.reset();
      }, 300);
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogTrigger asChild>
        <Button>
          <Plus className="mr-2 h-4 w-4" />
          Add Domain
        </Button>
      </DialogTrigger>
      <DialogContent className={createdDomain ? "sm:max-w-2xl" : "sm:max-w-[425px]"}>
        <DialogHeader>
          <DialogTitle>{createdDomain ? "Verify Domain Ownership" : "Add Custom Domain"}</DialogTitle>
          <DialogDescription>
            {createdDomain
              ? "Please configure the following DNS records to verify your domain."
              : "Enter the domain name you want to use for your aliases."}
          </DialogDescription>
        </DialogHeader>

        {createdDomain ? (
          <div className="space-y-6 pt-2">
            <DNSInstructions 
              domainId={createdDomain.id}
              domainName={createdDomain.domain_name} 
              verificationToken={createdDomain.verification_token} 
            />
            <DialogFooter className="sm:justify-end">
              <Button onClick={() => handleClose(false)}>Done</Button>
            </DialogFooter>
          </div>
        ) : (
          <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4 py-4">
            <div className="grid gap-2">
              <Label htmlFor="domain_name">Domain Name</Label>
              <Input
                id="domain_name"
                placeholder="example.com"
                {...form.register("domain_name")}
              />
              {form.formState.errors.domain_name && (
                <p className="text-sm text-destructive">
                  {form.formState.errors.domain_name.message}
                </p>
              )}
            </div>
            <DialogFooter>
              <Button type="submit" disabled={isSubmitting}>
                {isSubmitting && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                Add Domain
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}