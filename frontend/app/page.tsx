"use client";

import { useAuth } from "@/contexts/auth-context";
import { Navbar } from "@/components/landing/navbar";
import { HeroSection } from "@/components/landing/hero-section";
import { FeaturesSection } from "@/components/landing/features-section";
import { CTASection } from "@/components/landing/cta-section";
import Link from "next/link";

export default function Home() {
  const { isAuthenticated } = useAuth();

  return (
    <div className="flex min-h-screen flex-col">
      <Navbar />
      <main className="flex-1">
        <HeroSection isAuthenticated={isAuthenticated} />
        <FeaturesSection />
        <CTASection isAuthenticated={isAuthenticated} />
      </main>
      <footer className="py-6 w-full shrink-0 border-t">
        <div className="container px-4 md:px-6 mx-auto flex flex-col sm:flex-row items-center justify-between gap-4">
          <p className="text-xs text-muted-foreground">
            © 2026 Persistent Temp Mail. All rights reserved.
          </p>
          <nav className="flex gap-4 sm:gap-6">
            <Link className="text-xs hover:underline underline-offset-4 text-muted-foreground hover:text-foreground transition-colors" href="#">
              Terms of Service
            </Link>
            <Link className="text-xs hover:underline underline-offset-4 text-muted-foreground hover:text-foreground transition-colors" href="#">
              Privacy
            </Link>
          </nav>
        </div>
      </footer>
    </div>
  );
}