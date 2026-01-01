import { DomainList } from "@/components/domain/domain-list";
import { Metadata } from "next";

export const metadata: Metadata = {
  title: "Domains - Persistent Temp Mail",
  description: "Manage custom domains",
};

export default function DomainsPage() {
  return (
    <div className="h-full flex-1 flex-col space-y-8 p-8 md:flex">
      <DomainList />
    </div>
  );
}