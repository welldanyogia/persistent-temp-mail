import { EmailList } from "@/components/email/email-list";
import { Metadata } from "next";

export const metadata: Metadata = {
  title: "Inbox - Persistent Temp Mail",
  description: "Manage your received emails",
};

export default function InboxPage() {
  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-2">
        <h1 className="text-3xl font-bold tracking-tight">Inbox</h1>
        <p className="text-muted-foreground">
          View and manage emails sent to your temporary aliases.
        </p>
      </div>

      <EmailList />
    </div>
  );
}
