"use client";

import { AlertTriangle, WifiOff, FileX, ShieldAlert, ServerCrash, RefreshCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export interface ErrorStateProps {
  error: Error | { message: string; status?: number };
  reset?: () => void;
  className?: string;
}

export function ErrorState({ error, reset, className }: ErrorStateProps) {
  let icon: "error" | "offline" | "permission" | "not-found" | "server" = "error";
  let title = "Something went wrong";
  let description = error.message || "An unexpected error occurred. Please try again.";

  // Handle specific error cases if status is available or message contains specific keywords
  // This is a simple heuristic; ideally, we'd use error codes.
  const status = (error as any).status;
  const message = description.toLowerCase();

  if (status === 404 || message.includes("not found")) {
    icon = "not-found";
    title = "Resource Not Found";
    description = "The requested resource could not be found.";
  } else if (status === 403 || status === 401 || message.includes("permission") || message.includes("unauthorized")) {
    icon = "permission";
    title = "Access Denied";
    description = "You do not have permission to access this resource.";
  } else if (status === 503 || message.includes("network") || message.includes("offline") || message.includes("failed to fetch")) {
    icon = "offline";
    title = "Connection Failed";
    description = "Please check your internet connection and try again.";
  } else if (status && status >= 500) {
    icon = "server";
    title = "Server Error";
    description = "Our servers are experiencing issues. Please try again later.";
  }

  const icons = {
    error: AlertTriangle,
    offline: WifiOff,
    permission: ShieldAlert,
    "not-found": FileX,
    server: ServerCrash,
  };

  const Icon = icons[icon];

  return (
    <div className={cn("flex flex-col items-center justify-center p-8 text-center min-h-[300px]", className)}>
      <div className="flex h-20 w-20 items-center justify-center rounded-full bg-destructive/10 mb-6">
        <Icon className="h-10 w-10 text-destructive" aria-hidden="true" />
      </div>
      <h3 className="text-lg font-semibold tracking-tight">{title}</h3>
      <p className="text-sm text-muted-foreground mt-2 max-w-sm">{description}</p>
      {reset && (
        <Button onClick={reset} className="mt-6" variant="outline">
          <RefreshCcw className="mr-2 h-4 w-4" />
          Try Again
        </Button>
      )}
    </div>
  );
}
