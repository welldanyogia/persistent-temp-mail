"use client";

import { EmailListItem as EmailListItemType } from "@/types/email";
import { cn, formatDate } from "@/lib/utils";
import { Checkbox } from "@/components/ui/checkbox";
import { Paperclip, Trash2 } from "lucide-react";
import Link from "next/link";
import { useState, useRef } from "react";

interface EmailListItemProps {
  email: EmailListItemType;
  selected: boolean;
  onSelect: (id: string) => void;
  onDelete?: (id: string) => void;
}

export function EmailListItem({ email, selected, onSelect, onDelete }: EmailListItemProps) {
  const [swipeOffset, setSwipeOffset] = useState(0);
  const touchStart = useRef<number | null>(null);
  const isSwiping = useRef(false);

  const onTouchStart = (e: React.TouchEvent) => {
    touchStart.current = e.touches[0]?.clientX ?? null;
    isSwiping.current = false;
  };

  const onTouchMove = (e: React.TouchEvent) => {
    if (touchStart.current === null) return;
    const currentX = e.touches[0]?.clientX;
    if (currentX === undefined) return;
    const diff = touchStart.current - currentX;

    // Only swipe left, and limit to 80px
    if (diff > 10) {
      isSwiping.current = true;
      setSwipeOffset(Math.min(diff, 80));
    } else {
      setSwipeOffset(0);
    }
  };

  const onTouchEnd = () => {
    if (swipeOffset > 50) {
      setSwipeOffset(80);
    } else {
      setSwipeOffset(0);
    }
    touchStart.current = null;
  };

  const handleDelete = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    onDelete?.(email.id);
    setSwipeOffset(0);
  };

  return (
    <div className="relative overflow-hidden border-b last:border-0">
      {/* Background Delete Action */}
      <div
        className="absolute inset-0 bg-destructive flex items-center justify-end px-6 transition-opacity"
        style={{ opacity: swipeOffset > 20 ? 1 : 0 }}
      >
        <button
          onClick={handleDelete}
          className="text-destructive-foreground flex flex-col items-center gap-1"
        >
          <Trash2 className="h-5 w-5" />
          <span className="text-[10px] font-bold uppercase">Delete</span>
        </button>
      </div>

      <div
        className={cn(
          "group flex items-center gap-2 sm:gap-4 px-3 sm:px-4 py-3 bg-card transition-transform duration-200 ease-out relative z-10",
          !email.is_read && "bg-primary/5"
        )}
        style={{ transform: `translateX(-${swipeOffset}px)` }}
        onTouchStart={onTouchStart}
        onTouchMove={onTouchMove}
        onTouchEnd={onTouchEnd}
      >
        <div className="flex items-center" onClick={(e) => e.stopPropagation()}>
          <Checkbox
            checked={selected}
            onCheckedChange={() => onSelect(email.id)}
            aria-label={`Select email from ${email.from_name || email.from_address}`}
            className="h-5 w-5"
          />
        </div>

        <Link
          href={`/inbox/${email.id}`}
          className="flex-1 flex flex-col md:flex-row md:items-center gap-1 md:gap-4 min-w-0 py-1"
          onClick={(e) => isSwiping.current && e.preventDefault()}
        >
          <div className="md:w-64 shrink-0 min-w-0">
            <p
              className={cn(
                "text-sm truncate",
                !email.is_read ? "font-bold text-foreground" : "text-muted-foreground"
              )}
            >
              {email.from_name || email.from_address}
            </p>
          </div>

          <div className="flex-1 min-w-0 flex items-center gap-2">
            <div className="flex flex-col md:flex-row md:items-center gap-1 md:gap-2 min-w-0">
              <p
                className={cn(
                  "text-sm truncate",
                  !email.is_read ? "font-semibold text-foreground" : "text-muted-foreground"
                )}
              >
                {email.subject || "(No Subject)"}
              </p>
              <div className="flex items-center gap-2 shrink-0">
                {email.has_attachments && (
                  <Paperclip className="h-3.5 w-3.5 text-muted-foreground" />
                )}
                {email.preview_text && (
                  <span className="text-xs sm:text-sm text-muted-foreground truncate hidden sm:inline">
                    — {email.preview_text}
                  </span>
                )}
              </div>
            </div>
          </div>

          <div className="md:w-32 shrink-0 md:text-right mt-1 md:mt-0">
            <p className="text-[10px] sm:text-xs text-muted-foreground whitespace-nowrap">
              {formatDate(email.received_at)}
            </p>
          </div>
        </Link>
      </div>
    </div>
  );
}
