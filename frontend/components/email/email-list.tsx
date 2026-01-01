"use client";

import { useState, useEffect } from "react";
import { EmailListItem } from "./email-list-item";
import { useEmails } from "@/hooks/use-emails";
import { useAliases } from "@/hooks/use-aliases";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Calendar } from "@/components/ui/calendar";
import { Skeleton } from "@/components/ui/skeleton";
import { 
  Trash2, 
  CheckCheck, 
  Search, 
  Filter, 
  Calendar as CalendarIcon, 
  RotateCw,
  Inbox
} from "lucide-react";
import { format } from "date-fns";
import { DateRange } from "react-day-picker";
import { cn } from "@/lib/utils";

export function EmailList() {
  const {
    emails,
    isLoading,
    total,
    hasMore,
    loadMore,
    refresh,
    updateFilters,
    filters,
    selectedIds,
    toggleSelection,
    selectAll,
    deleteEmails,
    markAsRead,
  } = useEmails();

  const { aliases } = useAliases();
  
  const [searchTerm, setSearchTerm] = useState(filters.search || "");
  const [dateRange, setDateRange] = useState<DateRange | undefined>(undefined);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);

  // Debounce search
  useEffect(() => {
    const timer = setTimeout(() => {
      if (searchTerm !== filters.search) {
        updateFilters({ search: searchTerm });
      }
    }, 300);
    return () => clearTimeout(timer);
  }, [searchTerm, filters.search, updateFilters]);

  const handleDateChange = (range: DateRange | undefined) => {
    setDateRange(range);
    updateFilters({
      from_date: range?.from ? range.from.toISOString() : "",
      to_date: range?.to ? range.to.toISOString() : "",
    });
  };

  const handleDeleteClick = () => {
    if (selectedIds.length > 0) {
      setShowDeleteConfirm(true);
    }
  };

  const confirmDelete = async () => {
    await deleteEmails(selectedIds);
    setShowDeleteConfirm(false);
  };

  return (
    <div className="space-y-4">
      {/* Search and Filters Toolbar */}
      <div className="flex flex-col md:flex-row gap-4 items-start md:items-center justify-between">
        <div className="flex flex-1 w-full max-w-sm items-center space-x-2">
          <div className="relative w-full">
            <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
            <Input
              placeholder="Search emails..."
              className="pl-8"
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
            />
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {/* Alias Filter */}
          <Select
            value={filters.alias_id || "all"}
            onValueChange={(val) => updateFilters({ alias_id: val === "all" ? "" : val })}
          >
            <SelectTrigger className="w-[180px]">
              <Filter className="h-4 w-4 mr-2" />
              <SelectValue placeholder="All Aliases" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All Aliases</SelectItem>
              {aliases.map((alias) => (
                <SelectItem key={alias.id} value={alias.id}>
                  {alias.email_address}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          {/* Date Picker */}
          <Popover>
            <PopoverTrigger asChild>
              <Button variant="outline" className={cn("justify-start text-left font-normal", !dateRange && "text-muted-foreground")}>
                <CalendarIcon className="mr-2 h-4 w-4" />
                {dateRange?.from ? (
                  dateRange.to ? (
                    <>
                      {format(dateRange.from, "LLL dd")} - {format(dateRange.to, "LLL dd")}
                    </>
                  ) : (
                    format(dateRange.from, "LLL dd, y")
                  )
                ) : (
                  <span>Pick a date</span>
                )}
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-auto p-0" align="end">
              <Calendar
                initialFocus
                mode="range"
                defaultMonth={dateRange?.from}
                selected={dateRange || undefined}
                onSelect={handleDateChange}
                numberOfMonths={2}
              />
            </PopoverContent>
          </Popover>

          <Button variant="ghost" size="icon" onClick={refresh} title="Refresh">
            <RotateCw className={cn("h-4 w-4", isLoading && "animate-spin")} />
          </Button>
        </div>
      </div>

      {/* Bulk Actions Bar */}
      {selectedIds.length > 0 && (
        <div className="flex items-center justify-between bg-muted/50 p-2 rounded-lg border animate-in fade-in slide-in-from-top-1">
          <div className="flex items-center gap-4 px-2">
            <span className="text-sm font-medium">
              {selectedIds.length} selected
            </span>
            <div className="h-4 w-px bg-border" />
            <Button
              variant="ghost"
              size="sm"
              className="text-primary hover:text-primary hover:bg-primary/10"
              onClick={() => markAsRead(selectedIds)}
            >
              <CheckCheck className="h-4 w-4 mr-2" />
              Mark as Read
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="text-destructive hover:text-destructive hover:bg-destructive/10"
              onClick={handleDeleteClick}
            >
              <Trash2 className="h-4 w-4 mr-2" />
              Delete
            </Button>
          </div>
          <Button variant="ghost" size="sm" onClick={() => selectAll()}>
            Deselect All
          </Button>
        </div>
      )}

      {/* Email List Container */}
      <div className="rounded-lg border bg-card text-card-foreground shadow-sm overflow-hidden">
        <div className="bg-muted/30 px-4 py-2 border-b flex items-center gap-4">
          <Checkbox
            checked={emails.length > 0 && selectedIds.length === emails.length}
            onCheckedChange={selectAll}
            aria-label="Select all emails"
          />
          <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
            {total > 0 ? `${total} Emails` : "Inbox"}
          </span>
        </div>

        <div className="divide-y">
          {isLoading && emails.length === 0 ? (
            Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="flex items-center gap-4 p-4">
                <Skeleton className="h-4 w-4" />
                <div className="flex-1 space-y-2">
                  <Skeleton className="h-4 w-1/4" />
                  <Skeleton className="h-4 w-3/4" />
                </div>
                <Skeleton className="h-4 w-24" />
              </div>
            ))
          ) : emails.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-20 text-center">
              <div className="h-12 w-12 rounded-full bg-muted flex items-center justify-center mb-4">
                <Inbox className="h-6 w-6 text-muted-foreground" />
              </div>
              <h3 className="text-lg font-medium">No emails found</h3>
              <p className="text-sm text-muted-foreground max-w-xs mx-auto">
                {searchTerm || filters.alias_id || filters.from_date
                  ? "Try adjusting your filters or search term to find what you're looking for."
                  : "When you receive emails, they will appear here."}
              </p>
              {(searchTerm || filters.alias_id || filters.from_date) && (
                <Button 
                  variant="link" 
                  onClick={() => {
                    setSearchTerm("");
                    setDateRange(undefined);
                    updateFilters({ search: "", alias_id: "", from_date: "", to_date: "" });
                  }}
                >
                  Clear all filters
                </Button>
              )}
            </div>
          ) : (
            emails.map((email) => (
              <EmailListItem
                key={email.id}
                email={email}
                selected={selectedIds.includes(email.id)}
                onSelect={toggleSelection}
                onDelete={(id) => deleteEmails([id])}
              />
            ))
          )}
        </div>

        {/* Load More */}
        {hasMore && (
          <div className="p-4 border-t text-center">
            <Button
              variant="ghost"
              size="sm"
              onClick={loadMore}
              disabled={isLoading}
              className="w-full max-w-xs"
            >
              {isLoading ? (
                <RotateCw className="h-4 w-4 mr-2 animate-spin" />
              ) : null}
              Load More Emails
            </Button>
          </div>
        )}
      </div>

      {/* Delete Confirmation Dialog */}
      <AlertDialog open={showDeleteConfirm} onOpenChange={setShowDeleteConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Are you absolutely sure?</AlertDialogTitle>
            <AlertDialogDescription>
              This action will permanently delete {selectedIds.length} email(s).
              This action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              onClick={confirmDelete}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
