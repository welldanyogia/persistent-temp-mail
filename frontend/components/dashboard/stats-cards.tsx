"use client";

import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { DashboardStats } from "@/types/dashboard";
import { dashboardService } from "@/lib/api/dashboard";
import { formatSize } from "@/lib/utils";
import { Mail, Shield, AlertCircle, HardDrive } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";

export function StatsCards() {
  const [stats, setStats] = useState<DashboardStats | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchStats = async () => {
      try {
        const data = await dashboardService.getStats();
        setStats(data);
      } catch (err) {
        console.error("Failed to fetch stats:", err);
        setError("Failed to load statistics");
      } finally {
        setIsLoading(false);
      }
    };

    fetchStats();
  }, []);

  if (error) {
    return (
      <div className="p-4 rounded-lg bg-destructive/10 text-destructive text-sm">
        {error}
      </div>
    );
  }

  const items = [
    {
      title: "Total Aliases",
      value: stats?.total_aliases,
      icon: Shield,
      description: "Active email aliases",
    },
    {
      title: "Total Emails",
      value: stats?.total_emails,
      icon: Mail,
      description: "Received across all aliases",
    },
    {
      title: "Unread Emails",
      value: stats?.unread_emails,
      icon: AlertCircle,
      description: "Messages waiting for you",
    },
    {
      title: "Storage Used",
      value: stats ? formatSize(stats.storage_used_bytes) : null,
      icon: HardDrive,
      description: "Attachment storage",
    },
  ];

  return (
    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
      {items.map((item, index) => (
        <Card key={index}>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">
              {item.title}
            </CardTitle>
            <item.icon className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <div className="space-y-2">
                <Skeleton className="h-8 w-16" />
                <Skeleton className="h-4 w-24" />
              </div>
            ) : (
              <>
                <div className="text-2xl font-bold">{item.value ?? 0}</div>
                <p className="text-xs text-muted-foreground">
                  {item.description}
                </p>
              </>
            )}
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
