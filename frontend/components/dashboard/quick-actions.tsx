"use client";

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import Link from "next/link";
import { Plus, Globe } from "lucide-react";

export function QuickActions() {
  return (
    <Card className="h-full">
      <CardHeader>
        <CardTitle>Quick Actions</CardTitle>
        <CardDescription>
          Common tasks and management
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4 sm:grid-cols-2">
        <Button 
          variant="outline" 
          className="h-24 flex flex-col items-center justify-center gap-2 hover:border-primary hover:text-primary transition-colors"
          asChild
        >
          <Link href="/aliases?action=create">
            <Plus className="h-6 w-6" />
            <span>Create Alias</span>
          </Link>
        </Button>
        <Button 
          variant="outline" 
          className="h-24 flex flex-col items-center justify-center gap-2 hover:border-primary hover:text-primary transition-colors"
          asChild
        >
          <Link href="/domains?action=add">
            <Globe className="h-6 w-6" />
            <span>Add Domain</span>
          </Link>
        </Button>
      </CardContent>
    </Card>
  );
}
