"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/contexts/auth-context";
import { Loader2 } from "lucide-react";

import { Header } from "@/components/layout/header";
import { Sidebar } from "@/components/layout/sidebar";
import { Footer } from "@/components/layout/footer";

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const { isAuthenticated, isLoading } = useAuth();
  const router = useRouter();

  useEffect(() => {
    if (!isLoading && !isAuthenticated) {
      router.push("/login");
    }
  }, [isLoading, isAuthenticated, router]);

  if (isLoading) {
    return (
      <div className="flex h-screen w-full items-center justify-center">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
      </div>
    );
  }

  if (!isAuthenticated) {
    return null; 
  }

  return (
    <div className="flex h-screen overflow-hidden bg-background">
      {/* Desktop Sidebar - Hidden on mobile, icons on tablet, full on desktop */}
      <div className="hidden md:flex w-16 lg:w-64 flex-col border-r bg-card h-full transition-all duration-300 ease-in-out">
        <Sidebar className="border-none" />
      </div>

      <div className="flex flex-1 flex-col h-full overflow-hidden">
        <Header />
        
        <main 
          id="main-content" 
          role="main" 
          aria-label="Main content" 
          className="flex-1 overflow-y-auto p-4 md:p-8 focus:outline-none" 
          tabIndex={-1}
        >
          <div className="mx-auto max-w-6xl h-full flex flex-col">
            {children}
          </div>
        </main>
        
        <Footer />
      </div>
    </div>
  );
}
