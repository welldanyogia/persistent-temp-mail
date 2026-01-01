"use client";

import { motion } from "framer-motion";
import { Button } from "@/components/ui/button";
import Link from "next/link";
import { ArrowRight, ShieldCheck, Zap } from "lucide-react";

export function HeroSection({ isAuthenticated }: { isAuthenticated: boolean }) {
  return (
    <section className="relative overflow-hidden pt-32 pb-20 md:pt-48 md:pb-32">
      {/* Background gradients */}
      <div className="absolute top-0 left-1/2 -translate-x-1/2 w-full h-full -z-10 pointer-events-none">
        <div className="absolute top-0 left-1/4 w-[500px] h-[500px] bg-primary/20 rounded-full blur-[100px] opacity-50 dark:opacity-30 mix-blend-multiply animate-blob" />
        <div className="absolute top-0 right-1/4 w-[500px] h-[500px] bg-purple-500/20 rounded-full blur-[100px] opacity-50 dark:opacity-30 mix-blend-multiply animate-blob animation-delay-2000" />
        <div className="absolute -bottom-8 left-1/2 w-[500px] h-[500px] bg-pink-500/20 rounded-full blur-[100px] opacity-50 dark:opacity-30 mix-blend-multiply animate-blob animation-delay-4000" />
      </div>

      <div className="container px-4 md:px-6 mx-auto">
        <div className="flex flex-col items-center text-center space-y-8">
          <motion.div
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.5 }}
            className="inline-flex items-center rounded-full border px-3 py-1 text-sm font-medium backdrop-blur-sm bg-background/50"
          >
            <span className="flex h-2 w-2 rounded-full bg-green-500 mr-2 animate-pulse" />
            v1.0 Now Available
          </motion.div>

          <motion.h1
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.5, delay: 0.1 }}
            className="text-4xl md:text-6xl lg:text-7xl font-bold tracking-tight bg-clip-text text-transparent bg-gradient-to-b from-foreground to-foreground/70"
          >
            Your Inbox, <br className="hidden md:block" />
            <span className="text-primary">Permanent Control.</span>
          </motion.h1>

          <motion.p
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.5, delay: 0.2 }}
            className="max-w-[700px] text-lg md:text-xl text-muted-foreground leading-relaxed"
          >
            Create disposable email addresses that last forever. Secure, private, 
            and instant. Stop spam before it starts.
          </motion.p>

          <motion.div
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.5, delay: 0.3 }}
            className="flex flex-col sm:flex-row gap-4 w-full sm:w-auto"
          >
            <Link href={isAuthenticated ? "/dashboard" : "/register"} className="w-full sm:w-auto">
              <Button size="lg" className="w-full sm:w-auto h-12 px-8 text-base shadow-lg shadow-primary/20 transition-all hover:shadow-primary/40 hover:-translate-y-0.5">
                {isAuthenticated ? "Go to Dashboard" : "Get Started for Free"}
                <ArrowRight className="ml-2 h-4 w-4" />
              </Button>
            </Link>
            {!isAuthenticated && (
              <Link href="/login" className="w-full sm:w-auto">
                <Button variant="outline" size="lg" className="w-full sm:w-auto h-12 px-8 text-base backdrop-blur-sm bg-background/50">
                  Sign In
                </Button>
              </Link>
            )}
          </motion.div>

          {/* Stats / Trust indicators */}
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            transition={{ duration: 1, delay: 0.5 }}
            className="pt-12 grid grid-cols-2 md:grid-cols-3 gap-8 md:gap-16 text-center border-t border-border/50 w-full max-w-3xl mt-8"
          >
            <div className="space-y-1">
              <h4 className="text-2xl md:text-3xl font-bold">100%</h4>
              <p className="text-sm text-muted-foreground">Privacy Focused</p>
            </div>
            <div className="space-y-1">
              <h4 className="text-2xl md:text-3xl font-bold">0ms</h4>
              <p className="text-sm text-muted-foreground">Latency (SSE)</p>
            </div>
            <div className="space-y-1 col-span-2 md:col-span-1">
              <h4 className="text-2xl md:text-3xl font-bold">Unlimited</h4>
              <p className="text-sm text-muted-foreground">Duration</p>
            </div>
          </motion.div>
        </div>
      </div>
    </section>
  );
}
