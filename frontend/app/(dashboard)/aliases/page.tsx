import { AliasList } from "@/components/alias/alias-list";
import { Metadata } from "next";

export const metadata: Metadata = {
  title: "Aliases - Persistent Temp Mail",
  description: "Manage your email aliases",
};

export default function AliasesPage() {
  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-2">
        <h1 className="text-3xl font-bold tracking-tight">Aliases</h1>
        <p className="text-muted-foreground">
          Create and manage your temporary email addresses.
        </p>
      </div>

      <AliasList />
    </div>
  );
}
