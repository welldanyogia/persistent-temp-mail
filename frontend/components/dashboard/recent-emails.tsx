"use client";

import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { emailService } from "@/lib/api/emails";
import { EmailListItem } from "@/types/email";
import { formatDate } from "@/lib/utils";
import { Skeleton } from "@/components/ui/skeleton";
import Link from "next/link";
import { ArrowRight, Mail } from "lucide-react";

export function RecentEmails() {
  const [emails, setEmails] = useState<EmailListItem[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchEmails = async () => {
      try {
        const response = await emailService.list({ limit: 5 });
        setEmails(response.emails);
      } catch (err) {
        console.error("Failed to fetch recent emails:", err);
        setError("Failed to load recent emails");
      } finally {
        setIsLoading(false);
      }
    };

    fetchEmails();
  }, []);

  if (error) {
    return (
      <Card className="h-full">
        <CardHeader>
          <CardTitle>Recent Emails</CardTitle>
        </CardHeader>
        <CardContent>
           <div className="p-4 rounded-lg bg-destructive/10 text-destructive text-sm">
            {error}
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="h-full flex flex-col">
      <CardHeader>
        <CardTitle>Recent Emails</CardTitle>
        <CardDescription>
          Your latest received messages
        </CardDescription>
      </CardHeader>
      <CardContent className="flex-1">
        {isLoading ? (
          <div className="space-y-4">
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="flex items-center space-x-4">
                <Skeleton className="h-10 w-10 rounded-full" />
                <div className="space-y-2 flex-1">
                  <Skeleton className="h-4 w-3/4" />
                  <Skeleton className="h-3 w-1/2" />
                </div>
              </div>
            ))}
          </div>
        ) : emails.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-48 text-muted-foreground text-center">
             <Mail className="h-10 w-10 mb-2 opacity-20" />
             <p>No emails yet</p>
          </div>
        ) : (
          <div className="space-y-4">
            {emails.map((email) => (
              <Link 
                key={email.id} 
                href={`/inbox/${email.id}`}
                className="flex items-start justify-between gap-4 p-3 rounded-lg hover:bg-muted/50 transition-colors border border-transparent hover:border-border"
              >
                <div className="flex-1 min-w-0">
                  <p className="font-medium truncate">
                    {email.subject || "(No Subject)"}
                  </p>
                  <p className="text-sm text-muted-foreground truncate">
                    {email.from_name || email.from_address}
                  </p>
                </div>
                <div className="text-xs text-muted-foreground whitespace-nowrap">
                  {formatDate(email.received_at)}
                </div>
              </Link>
            ))}
          </div>
        )}
      </CardContent>
      <div className="p-4 pt-0 mt-auto">
        <Button variant="outline" className="w-full" asChild>
          <Link href="/inbox">
            View All Emails
            <ArrowRight className="ml-2 h-4 w-4" />
          </Link>
        </Button>
      </div>
    </Card>
  );
}
