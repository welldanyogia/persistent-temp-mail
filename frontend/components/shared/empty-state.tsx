"use client";

import { AlertTriangle, WifiOff, FileX, ShieldAlert, ServerCrash, RefreshCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

interface EmptyStateProps {
  icon?: "inbox" | "search" | "error" | "offline" | "permission" | "not-found" | "server";
  title: string;
  description: string;
  action?: {
    label: string;
    onClick: () => void;
  };
  className?: string;
}

const icons = {
  inbox: FileX,
  search: FileX,
  error: AlertTriangle,
  offline: WifiOff,
  permission: ShieldAlert,
  "not-found": FileX,
  server: ServerCrash,
};

export function EmptyState({
  icon = "inbox",
  title,
  description,
  action,
  className,
}: EmptyStateProps) {
  const Icon = icons[icon] || FileX;

  return (
    <div className={cn("flex flex-col items-center justify-center p-8 text-center", className)}>
      <div className="flex h-20 w-20 items-center justify-center rounded-full bg-muted/50 mb-6">
        <Icon className="h-10 w-10 text-muted-foreground" aria-hidden="true" />
      </div>
      <h3 className="text-lg font-semibold tracking-tight">{title}</h3>
      <p className="text-sm text-muted-foreground mt-2 max-w-sm">{description}</p>
      {action && (
        <Button onClick={action.onClick} className="mt-6" variant="outline">
          {icon === "error" || icon === "offline" || icon === "server" ? (
            <RefreshCcw className="mr-2 h-4 w-4" />
          ) : null}
          {action.label}
        </Button>
      )}
    </div>
  );
}
