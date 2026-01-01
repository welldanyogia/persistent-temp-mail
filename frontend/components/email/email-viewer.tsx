"use client";

import { useState } from 'react';
import { sanitizeHtml } from '@/lib/utils/sanitize-html';
import { Button } from '@/components/ui/button';
import { AttachmentList } from './attachment-list';
import { formatDate } from '@/lib/utils';
import { AlertTriangle, Trash2, ArrowLeft, Reply, Forward, MoreVertical } from 'lucide-react';
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar';
import { Separator } from '@/components/ui/separator';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { useRouter } from 'next/navigation';
import { emailService } from '@/lib/api/emails';
import { toast } from 'sonner';
import Link from 'next/link';
import { Email } from '@/types/email';

interface EmailViewerProps {
  email: Email;
}

export function EmailViewer({ email }: EmailViewerProps) {
  const [showExternalImages, setShowExternalImages] = useState(false);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const router = useRouter();

  // Sanitize HTML content
  // If body_html is missing, wrap body_text in basic HTML structure
  const htmlContent = email.body_html || 
    (email.body_text ? `<div style="white-space: pre-wrap; font-family: monospace;">${email.body_text}</div>` : '<i>No content</i>');

  const sanitizedHtml = sanitizeHtml(htmlContent, {
    allowExternalImages: showExternalImages,
  });

  // Simple heuristic to detect if there might be blocked external images
  // This isn't perfect but covers common src="http..." cases
  const hasExternalImages = !showExternalImages && 
    (htmlContent.includes('src="http:') || htmlContent.includes('src="https:')) &&
    !htmlContent.includes('data:image/');

  const handleDelete = async () => {
    setIsDeleting(true);
    try {
      await emailService.delete(email.id);
      toast.success("Email deleted");
      router.push('/inbox');
    } catch (error: unknown) {
      const msg = error instanceof Error ? error.message : "Failed to delete email";
      toast.error(msg);
      setIsDeleting(false);
      setShowDeleteConfirm(false);
    }
  };

  const initial = (email.from_name || email.from_address).charAt(0).toUpperCase();

  return (
    <div className="flex flex-col h-full bg-card rounded-lg border shadow-sm overflow-hidden">
      {/* Toolbar */}
      <div className="flex items-center justify-between p-4 border-b bg-muted/20">
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="icon" asChild title="Back to Inbox">
            <Link href="/inbox">
              <ArrowLeft className="h-4 w-4" />
            </Link>
          </Button>
          <div className="h-6 w-px bg-border mx-2" />
          <Button variant="ghost" size="icon" title="Reply (Not Implemented)" disabled>
            <Reply className="h-4 w-4" />
          </Button>
          <Button variant="ghost" size="icon" title="Forward (Not Implemented)" disabled>
            <Forward className="h-4 w-4" />
          </Button>
        </div>
        <div className="flex items-center gap-2">
           <Button variant="ghost" size="icon" className="text-destructive hover:text-destructive hover:bg-destructive/10" onClick={() => setShowDeleteConfirm(true)} title="Delete">
            <Trash2 className="h-4 w-4" />
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon">
                <MoreVertical className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={() => navigator.clipboard.writeText(htmlContent)}>
                Copy Raw Source
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <a href={`data:text/html;charset=utf-8,${encodeURIComponent(htmlContent)}`} download={`email-${email.id}.html`}>
                  Download HTML
                </a>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      <div className="flex-1 overflow-auto p-4 sm:p-6 space-y-6">
        {/* Header */}
        <div className="space-y-4">
          <h1 className="text-xl sm:text-2xl font-bold tracking-tight break-words">{email.subject || '(No Subject)'}</h1>
          
          <div className="flex flex-col sm:flex-row sm:items-start justify-between gap-4">
            <div className="flex items-center gap-3">
              <Avatar className="h-10 w-10 shrink-0">
                <AvatarImage src={`https://www.gravatar.com/avatar/${email.from_address}?d=404`} />
                <AvatarFallback className="bg-primary/10 text-primary">{initial}</AvatarFallback>
              </Avatar>
              <div className="grid gap-0.5 min-w-0">
                <div className="font-semibold text-sm truncate">
                  {email.from_name ? (
                    <span className="mr-2">{email.from_name}</span>
                  ) : null}
                  <span className="text-muted-foreground font-normal text-xs">&lt;{email.from_address}&gt;</span>
                </div>
                <div className="text-xs text-muted-foreground truncate">
                  To: <span className="text-foreground">{email.alias_email}</span>
                </div>
              </div>
            </div>
            <div className="text-[10px] sm:text-xs text-muted-foreground whitespace-nowrap sm:mt-1">
              {formatDate(email.received_at)}
            </div>
          </div>
        </div>

        <Separator />

        {/* External Images Warning */}
        {hasExternalImages && (
          <div className="flex items-center gap-3 rounded-lg border border-yellow-500/30 bg-yellow-500/10 p-3 text-sm">
            <AlertTriangle className="h-4 w-4 text-yellow-500 shrink-0" />
            <div className="flex-1">
              <span className="font-medium text-yellow-600 dark:text-yellow-500">External images blocked.</span>
              <span className="text-muted-foreground ml-1">Images were blocked to protect your privacy.</span>
            </div>
            <Button
              variant="outline"
              size="sm"
              className="h-8 text-xs bg-background"
              onClick={() => setShowExternalImages(true)}
            >
              Load Images
            </Button>
          </div>
        )}

        {/* Email Body */}
        <div className="bg-card rounded-md">
           <div
            className="email-content prose prose-sm max-w-none dark:prose-invert break-words"
            dangerouslySetInnerHTML={{ __html: sanitizedHtml }}
          />
        </div>

        {/* Attachments */}
        {email.attachments && email.attachments.length > 0 && (
          <AttachmentList attachments={email.attachments} />
        )}
      </div>

      {/* Delete Confirmation Dialog */}
      <AlertDialog open={showDeleteConfirm} onOpenChange={setShowDeleteConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete this email?</AlertDialogTitle>
            <AlertDialogDescription>
              This action cannot be undone. This will permanently delete this email and all its attachments.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={isDeleting}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              onClick={handleDelete}
              disabled={isDeleting}
            >
              {isDeleting ? "Deleting..." : "Delete"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
