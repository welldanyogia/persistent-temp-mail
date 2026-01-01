"use client";

import { Alias } from "@/types/alias";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { formatDistanceToNow } from "date-fns";
import { Trash2, Mail, Copy, Check } from "lucide-react";
import { useState } from "react";
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
import { toast } from "sonner";
import { cn } from "@/lib/utils";

interface AliasCardProps {
  alias: Alias;
  onToggle: (id: string, currentStatus: boolean) => void;
  onDelete: (id: string) => void;
}

export function AliasCard({ alias, onToggle, onDelete }: AliasCardProps) {
  const [copied, setCopied] = useState(false);

  const copyToClipboard = async () => {
    try {
      await navigator.clipboard.writeText(alias.email_address);
      setCopied(true);
      toast.success("Address copied to clipboard");
      setTimeout(() => setCopied(false), 2000);
    } catch (err) {
      toast.error("Failed to copy address");
    }
  };

  return (
    <Card className={cn("transition-all", !alias.is_active && "opacity-75 bg-muted/30")}>
      <CardHeader className="p-4 flex flex-row items-start justify-between space-y-0">
        <div className="flex flex-col gap-1 min-w-0">
          <div className="flex items-center gap-2">
            <h3 className="font-semibold text-lg truncate" title={alias.email_address}>
              {alias.email_address}
            </h3>
            <Button
              variant="ghost"
              size="icon"
              className="h-9 w-9 shrink-0"
              onClick={copyToClipboard}
            >
              {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
            </Button>
          </div>
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Badge variant={alias.is_active ? "default" : "secondary"} className="h-5">
              {alias.is_active ? "Active" : "Inactive"}
            </Badge>
            <span>•</span>
            <span title={alias.created_at}>
              Created {formatDistanceToNow(new Date(alias.created_at), { addSuffix: true })}
            </span>
          </div>
        </div>
        <Switch
          checked={alias.is_active}
          onCheckedChange={() => onToggle(alias.id, !!alias.is_active)}
          aria-label="Toggle alias status"
          className="scale-110" // Larger tap target for switch
        />
      </CardHeader>
      <CardContent className="p-4 pt-0">
        <div className="flex items-center justify-between mt-4">
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Mail className="h-4 w-4" />
            <span>{alias.email_count} emails</span>
          </div>
          
          <AlertDialog>
            <AlertDialogTrigger asChild>
              <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive hover:bg-destructive/10 h-10 px-3">
                <Trash2 className="h-4 w-4 mr-2" />
                Delete
              </Button>
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>Delete Alias?</AlertDialogTitle>
                <AlertDialogDescription>
                  This will permanently delete <strong>{alias.email_address}</strong> and all associated emails. 
                  This action cannot be undone.
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>Cancel</AlertDialogCancel>
                <AlertDialogAction 
                  className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
                  onClick={() => onDelete(alias.id)}
                >
                  Delete
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        </div>
        {alias.description && (
          <p className="text-sm text-muted-foreground mt-3 border-t pt-3 truncate">
            {alias.description}
          </p>
        )}
      </CardContent>
    </Card>
  );
}
