"use client";

import { useState } from "react";
import { Domain } from "@/types/domain";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Trash2, ShieldCheck, ShieldAlert, RefreshCw, Eye, Globe } from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import { domainService } from "@/lib/api/domains";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { DNSInstructions } from "./dns-instructions";

interface DomainCardProps {
  domain: Domain;
  onUpdate: () => void;
}

export function DomainCard({ domain, onUpdate }: DomainCardProps) {
  const [isVerifying, setIsVerifying] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);

  const handleVerify = async () => {
    setIsVerifying(true);
    try {
      await domainService.verify(domain.id);
      toast.success("Verification triggered. Check status shortly.");
      onUpdate();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Verification failed";
      toast.error(msg);
    } finally {
      setIsVerifying(false);
    }
  };

  const handleDelete = async () => {
    setIsDeleting(true);
    try {
      await domainService.delete(domain.id);
      toast.success("Domain deleted");
      onUpdate();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Delete failed";
      toast.error(msg);
      setIsDeleting(false);
    }
  };

  const getStatusColor = (status: string) => {
    switch (status) {
      case "verified": return "default";
      case "failed": return "destructive";
      default: return "secondary"; // pending
    }
  };

  const getSSLIcon = () => {
    switch (domain.ssl_status) {
      case "active": return <ShieldCheck className="h-4 w-4 text-green-500" />;
      case "failed": return <ShieldAlert className="h-4 w-4 text-destructive" />;
      default: return <RefreshCw className="h-4 w-4 text-muted-foreground" />; // pending/provisioning
    }
  };

  return (
    <Card className="overflow-hidden transition-all hover:shadow-md">
      <CardHeader className="p-4 flex flex-row items-start justify-between space-y-0 bg-muted/10 border-b">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <div className="bg-background p-1.5 rounded-md border shadow-sm">
              <Globe className="h-4 w-4 text-muted-foreground" />
            </div>
            <h3 className="font-semibold text-lg">{domain.domain_name}</h3>
          </div>
          <div className="flex items-center gap-2 text-xs text-muted-foreground ml-9">
            <Badge variant={getStatusColor(domain.status)} className="capitalize h-5 px-2">
              {domain.status}
            </Badge>
            {domain.status === "verified" && (
              <>
                <span>•</span>
                <div className="flex items-center gap-1.5" title={`SSL: ${domain.ssl_status}`}>
                  {getSSLIcon()}
                  <span className="capitalize hidden sm:inline">{domain.ssl_status === 'active' ? 'SSL Active' : `${domain.ssl_status} SSL`}</span>
                </div>
              </>
            )}
          </div>
        </div>
        
        {domain.status !== "verified" && (
           <Dialog>
             <DialogTrigger asChild>
               <Button variant="outline" size="sm" className="h-8">
                 <Eye className="mr-2 h-3.5 w-3.5" />
                 Instructions
               </Button>
             </DialogTrigger>
             <DialogContent className="sm:max-w-2xl">
               <DialogHeader>
                 <DialogTitle>DNS Configuration for {domain.domain_name}</DialogTitle>
               </DialogHeader>
               <DNSInstructions 
                 domainId={domain.id}
                 domainName={domain.domain_name} 
                 verificationToken={domain.verification_token} 
               />
             </DialogContent>
           </Dialog>
        )}
      </CardHeader>
      <CardContent className="p-4">
        <div className="flex items-center justify-between">
          <div className="flex flex-col gap-1">
            <span className="text-sm font-medium">{domain.alias_count}</span>
            <span className="text-xs text-muted-foreground">active aliases</span>
          </div>
          
          <div className="flex items-center gap-2">
            {domain.status !== "verified" && (
              <Button 
                variant="secondary" 
                size="sm" 
                onClick={handleVerify} 
                disabled={isVerifying}
                className="h-10"
              >
                {isVerifying ? <RefreshCw className="h-4 w-4 animate-spin mr-2" /> : <RefreshCw className="h-4 w-4 mr-2" />}
                Verify DNS
              </Button>
            )}

            <AlertDialog>
              <AlertDialogTrigger asChild>
                <Button variant="ghost" size="icon" className="h-10 w-10 text-muted-foreground hover:text-destructive hover:bg-destructive/10">
                  <Trash2 className="h-5 w-5" />
                </Button>
              </AlertDialogTrigger>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>Delete Domain?</AlertDialogTitle>
                  <AlertDialogDescription>
                    This will permanently delete <strong>{domain.domain_name}</strong>.
                    <br /><br />
                    <span className="bg-destructive/10 text-destructive px-2 py-1 rounded text-sm font-medium border border-destructive/20 inline-block w-full text-center">
                      Warning: This will also delete {domain.alias_count} aliases and all their emails.
                    </span>
                    <br /><br />
                    This action cannot be undone.
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel>Cancel</AlertDialogCancel>
                  <AlertDialogAction
                    className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
                    onClick={handleDelete}
                  >
                    {isDeleting ? "Deleting..." : "Delete Domain"}
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
          </div>
        </div>
        <div className="text-[10px] uppercase tracking-wider text-muted-foreground/60 mt-4 font-medium">
          Added {formatDistanceToNow(new Date(domain.created_at), { addSuffix: true })}
        </div>
      </CardContent>
    </Card>
  );
}