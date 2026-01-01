"use client";

import { useEffect, useState, use } from "react";
import { EmailViewer } from "@/components/email/email-viewer";
import { emailService } from "@/lib/api/emails";
import { Email } from "@/types/email";
import { Loader2 } from "lucide-react";
import { notFound } from "next/navigation";

export default function EmailDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const resolvedParams = use(params); // Next.js 15/16 requires unwrapping params
  const [email, setEmail] = useState<Email | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchEmail = async () => {
      try {
        const data = await emailService.get(resolvedParams.id);
        setEmail(data);
        
        // Mark as read if not already
        if (!data.is_read) {
          try {
            await emailService.markAsRead(data.id);
          } catch (err) {
            console.error("Failed to mark as read", err);
          }
        }
      } catch (err: any) {
        console.error("Failed to fetch email", err);
        // If 404, we could use notFound() but that's server-side mostly.
        // On client, we just show error or redirect.
        setError(err.message || "Failed to load email");
        if (err.message?.includes("404")) {
           notFound(); // This might not work as expected in client component effect, but handy to signal intent
        }
      } finally {
        setIsLoading(false);
      }
    };

    fetchEmail();
  }, [resolvedParams.id]);

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center p-12">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
      </div>
    );
  }

  if (error || !email) {
    return (
      <div className="flex flex-col items-center justify-center h-[50vh] gap-4">
        <h2 className="text-xl font-semibold text-destructive">Error Loading Email</h2>
        <p className="text-muted-foreground">{error || "Email not found"}</p>
      </div>
    );
  }

  return <EmailViewer email={email} />;
}
