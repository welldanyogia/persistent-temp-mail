"use client";

import { motion } from "framer-motion";
import { Mail, Shield, Zap, Lock, Globe, Bell } from "lucide-react";
import { cn } from "@/lib/utils";

const features = [
  {
    title: "Instant Aliases",
    description: "Generate new email addresses in milliseconds. No limits, no waiting.",
    icon: Zap,
    className: "md:col-span-2",
    gradient: "from-yellow-500/20 via-orange-500/20 to-red-500/20",
  },
  {
    title: "Privacy First",
    description: "Tracking pixels are blocked by default. Your data is yours alone.",
    icon: Shield,
    className: "md:col-span-1",
    gradient: "from-blue-500/20 via-indigo-500/20 to-violet-500/20",
  },
  {
    title: "Real-time Updates",
    description: "Watch emails arrive instantly without refreshing. Powered by Server-Sent Events.",
    icon: Bell,
    className: "md:col-span-1",
    gradient: "from-green-500/20 via-emerald-500/20 to-teal-500/20",
  },
  {
    title: "Custom Domains",
    description: "Bring your own domain or use ours. Professional presence, temporary convenience.",
    icon: Globe,
    className: "md:col-span-2",
    gradient: "from-pink-500/20 via-rose-500/20 to-red-500/20",
  },
];

export function FeaturesSection() {
  return (
    <section className="py-24 relative">
      <div className="container px-4 md:px-6 mx-auto">
        <div className="text-center mb-16 space-y-4">
          <h2 className="text-3xl md:text-5xl font-bold tracking-tight">
            Everything you need, <span className="text-primary">nothing you don&apos;t.</span>
          </h2>
          <p className="text-muted-foreground text-lg max-w-[600px] mx-auto">
            We&apos;ve stripped away the bloat to focus on what matters: speed, privacy, and reliability.
          </p>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
          {features.map((feature, i) => (
            <motion.div
              key={i}
              initial={{ opacity: 0, y: 20 }}
              whileInView={{ opacity: 1, y: 0 }}
              viewport={{ once: true }}
              transition={{ duration: 0.5, delay: i * 0.1 }}
              className={cn(
                "group relative overflow-hidden rounded-3xl border bg-background/50 p-8 backdrop-blur-sm transition-all hover:border-foreground/20",
                feature.className
              )}
            >
              <div
                className={cn(
                  "absolute inset-0 opacity-0 group-hover:opacity-100 transition-opacity duration-500 bg-gradient-to-br",
                  feature.gradient
                )}
              />
              
              <div className="relative z-10 flex flex-col h-full">
                <div className="mb-6 inline-flex h-12 w-12 items-center justify-center rounded-xl bg-background/80 shadow-sm border ring-1 ring-black/5 dark:ring-white/10">
                  <feature.icon className="h-6 w-6" />
                </div>
                <h3 className="mb-2 text-xl font-bold">{feature.title}</h3>
                <p className="text-muted-foreground">{feature.description}</p>
              </div>
            </motion.div>
          ))}
        </div>
      </div>
    </section>
  );
}
