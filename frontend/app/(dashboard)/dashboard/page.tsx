"use client";

import { Suspense } from "react";
import dynamic from "next/dynamic";
import { QuickActions } from "@/components/dashboard/quick-actions";
import { StatsCardSkeleton, EmailListSkeleton } from "@/components/shared/loading-skeleton";

const StatsCards = dynamic(() => import("@/components/dashboard/stats-cards").then(mod => mod.StatsCards), {
  loading: () => (
    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
      <StatsCardSkeleton />
      <StatsCardSkeleton />
      <StatsCardSkeleton />
      <StatsCardSkeleton />
    </div>
  ),
});

const RecentEmails = dynamic(() => import("@/components/dashboard/recent-emails").then(mod => mod.RecentEmails), {
  loading: () => <EmailListSkeleton count={5} />,
});

export default function DashboardPage() {
  return (
    <div className="space-y-6">
      <h1 className="text-3xl font-bold">Dashboard</h1>
      <Suspense>
        <StatsCards />
      </Suspense>
      <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
        <div className="lg:col-span-2">
          <Suspense>
            <RecentEmails />
          </Suspense>
        </div>
        <div>
          <QuickActions />
        </div>
      </div>
    </div>
  );
}
