"use client";

import Link from "next/link";
import { useAuth } from "@/contexts/auth-context";
import { Button } from "@/components/ui/button";
import { Mail } from "lucide-react";
import { useEffect, useState } from "react";
import { cn } from "@/lib/utils";
import { ThemeToggle } from "@/components/shared/theme-toggle";

export function Navbar() {
  const { isAuthenticated } = useAuth();
  const [scrolled, setScrolled] = useState(false);

  useEffect(() => {
    const handleScroll = () => {
      setScrolled(window.scrollY > 20);
    };
    window.addEventListener("scroll", handleScroll);
    return () => window.removeEventListener("scroll", handleScroll);
  }, []);

  return (
    <header
      className={cn(
        "fixed top-0 w-full z-50 transition-all duration-300 border-b border-transparent",
        scrolled ? "bg-background/80 backdrop-blur-md border-border/50 shadow-sm" : ""
      )}
    >
      <div className="container px-4 md:px-6 h-16 flex items-center justify-between mx-auto">
        <Link className="flex items-center justify-center gap-2" href="/">
          <div className="bg-primary/10 p-2 rounded-lg">
            <Mail className="h-5 w-5 text-primary" />
          </div>
          <span className="font-bold text-lg hidden sm:block">Persistent Temp Mail</span>
        </Link>
        
        <nav className="flex items-center gap-4">
          <ThemeToggle />
          {isAuthenticated ? (
            <Link href="/dashboard">
              <Button>Dashboard</Button>
            </Link>
          ) : (
            <>
              <Link href="/login">
                <Button variant="ghost">Login</Button>
              </Link>
              <Link href="/register">
                <Button>Get Started</Button>
              </Link>
            </>
          )}
        </nav>
      </div>
    </header>
  );
}
