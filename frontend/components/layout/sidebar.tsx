"use client";

import { cn } from "@/lib/utils";
import { usePathname } from "next/navigation";
import Link from "next/link";
import { LayoutDashboard, Inbox, Mail, Globe, Settings, LogOut } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useAuth } from "@/contexts/auth-context";
import { Badge } from "@/components/ui/badge";

export const navItems = [
  {
    title: "Dashboard",
    href: "/dashboard",
    icon: LayoutDashboard,
  },
  {
    title: "Inbox",
    href: "/inbox",
    icon: Inbox,
    hasBadge: true,
  },
  {
    title: "Aliases",
    href: "/aliases",
    icon: Mail,
  },
  {
    title: "Domains",
    href: "/domains",
    icon: Globe,
  },
  {
    title: "Settings",
    href: "/settings",
    icon: Settings,
  },
];

interface SidebarProps extends React.HTMLAttributes<HTMLDivElement> {
  isMobile?: boolean;
  onNavigate?: () => void;
}

export function Sidebar({ className, isMobile, onNavigate, ...props }: SidebarProps) {
  const pathname = usePathname();
  const { logout } = useAuth();
  const unreadCount = 0; // Placeholder

  return (
    <div className={cn("flex flex-col h-full bg-card border-r", className)} {...props}>
      <div className="p-4 lg:p-6">
        {onNavigate ? (
          <Link 
            href="/dashboard" 
            className="flex items-center gap-2 font-bold text-xl" 
            onClick={() => onNavigate()}
          >
            <Mail className="h-6 w-6 text-primary shrink-0" />
            <span className={cn("truncate transition-all", !isMobile && "hidden lg:inline")}>TempMail</span>
          </Link>
        ) : (
          <Link 
            href="/dashboard" 
            className="flex items-center gap-2 font-bold text-xl"
          >
            <Mail className="h-6 w-6 text-primary shrink-0" />
            <span className={cn("truncate transition-all", !isMobile && "hidden lg:inline")}>TempMail</span>
          </Link>
        )}
      </div>
      
      <div 
        className="flex-1 px-2 lg:px-4 space-y-2 py-2"
        role="navigation"
        aria-label="Main navigation"
      >
        {navItems.map((item) => {
          const isActive = pathname === item.href || pathname.startsWith(`${item.href}/`);
          const linkProps = {
            key: item.href,
            href: item.href,
            "aria-current": isActive ? ("page" as const) : undefined,
            className: cn(
              "flex items-center gap-3 px-3 py-2 rounded-md text-sm font-medium transition-all group relative outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
              isActive
                ? "bg-primary/10 text-primary"
                : "text-muted-foreground hover:bg-muted hover:text-foreground"
            ),
            title: item.title,
            ...(onNavigate && { onClick: () => onNavigate() }),
          };
          return (
            <Link {...linkProps}>
              <item.icon className="h-5 w-5 shrink-0" />
              <span className={cn("truncate transition-all", !isMobile && "hidden lg:inline")}>
                {item.title}
              </span>
              {item.hasBadge && unreadCount > 0 && (
                <Badge 
                  variant="secondary" 
                  className={cn(
                    "ml-auto h-5 px-1.5 min-w-[1.25rem]",
                    !isMobile && "hidden lg:flex"
                  )}
                >
                  {unreadCount}
                </Badge>
              )}
              
              {/* Tooltip for collapsed state */}
              {!isMobile && (
                <div className="absolute left-full ml-2 px-2 py-1 bg-popover text-popover-foreground text-xs rounded border shadow-md opacity-0 group-hover:opacity-100 lg:group-hover:opacity-0 transition-opacity pointer-events-none z-50 whitespace-nowrap">
                  {item.title}
                </div>
              )}
            </Link>
          );
        })}
      </div>

      <div className="p-2 lg:p-4 border-t mt-auto">
        <Button
          variant="ghost"
          className="w-full justify-start gap-3 text-muted-foreground hover:text-destructive hover:bg-destructive/10 px-3 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
          aria-label="Logout"
          onClick={() => {
            logout();
            onNavigate?.();
          }}
        >
          <LogOut className="h-5 w-5 shrink-0" />
          <span className={cn("truncate transition-all", !isMobile && "hidden lg:inline")}>Logout</span>
        </Button>
      </div>
    </div>
  );
}
