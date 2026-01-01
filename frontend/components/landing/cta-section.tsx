"use client";

import { motion } from "framer-motion";
import { Button } from "@/components/ui/button";
import Link from "next/link";
import { ArrowRight } from "lucide-react";

export function CTASection({ isAuthenticated }: { isAuthenticated: boolean }) {
  return (
    <section className="py-24 relative overflow-hidden">
      <div className="absolute inset-0 bg-primary/5 -z-10" />
      <div className="container px-4 md:px-6 mx-auto">
        <motion.div
          initial={{ opacity: 0, scale: 0.95 }}
          whileInView={{ opacity: 1, scale: 1 }}
          viewport={{ once: true }}
          transition={{ duration: 0.5 }}
          className="flex flex-col items-center text-center space-y-8 max-w-3xl mx-auto"
        >
          <h2 className="text-3xl md:text-5xl font-bold tracking-tight">
            Ready to reclaim your inbox?
          </h2>
          <p className="text-lg text-muted-foreground">
            Join thousands of users who have stopped spam dead in its tracks. 
            No credit card required.
          </p>
          <div className="flex flex-col sm:flex-row gap-4">
            <Link href={isAuthenticated ? "/dashboard" : "/register"}>
              <Button size="lg" className="h-12 px-8 text-base shadow-xl hover:translate-y-[-2px] transition-transform">
                {isAuthenticated ? "Go to Dashboard" : "Create Free Account"}
                <ArrowRight className="ml-2 h-4 w-4" />
              </Button>
            </Link>
          </div>
        </motion.div>
      </div>
    </section>
  );
}
