"use client";

import { useState } from 'react';
import { Button } from '@/components/ui/button';
import { Download, Eye, FileIcon, Image, FileText } from 'lucide-react';
import { formatSize } from '@/lib/utils';
import { Dialog, DialogContent, DialogTrigger } from '@/components/ui/dialog';
import { Attachment } from '@/types/email';
import dynamic from 'next/dynamic';
import { Skeleton } from '@/components/ui/skeleton';

const AttachmentPreview = dynamic(() => import('./attachment-preview').then(mod => mod.AttachmentPreview), {
  loading: () => (
    <div className="w-full h-full flex items-center justify-center">
      <Skeleton className="w-full h-full" />
    </div>
  ),
  ssr: false, // Dialog content is client-side only anyway
});

interface AttachmentListProps {
  attachments: Attachment[];
}

export function AttachmentList({ attachments }: AttachmentListProps) {
  return (
    <div className="space-y-3 pt-4 border-t">
      <h3 className="font-medium text-sm text-muted-foreground">
        Attachments ({attachments.length})
      </h3>
      <div className="grid gap-2 sm:grid-cols-2 md:grid-cols-3">
        {attachments.map((attachment) => (
          <AttachmentItem key={attachment.id} attachment={attachment} />
        ))}
      </div>
    </div>
  );
}

function AttachmentItem({ attachment }: { attachment: Attachment }) {
  const isImage = attachment.content_type.startsWith('image/');
  const isPdf = attachment.content_type === 'application/pdf';
  const canPreview = isImage || isPdf;
  const [isOpen, setIsOpen] = useState(false);

  const getIcon = () => {
    if (isImage) return <Image className="h-5 w-5" />;
    if (isPdf) return <FileText className="h-5 w-5" />;
    return <FileIcon className="h-5 w-5" />;
  };

  return (
    <div className="flex items-center gap-3 rounded-lg border p-3 hover:bg-muted/30 transition-colors">
      <div className="flex h-10 w-10 items-center justify-center rounded bg-muted text-muted-foreground">
        {getIcon()}
      </div>
      <div className="flex-1 min-w-0">
        <p className="truncate text-sm font-medium" title={attachment.filename}>
          {attachment.filename}
        </p>
        <p className="text-xs text-muted-foreground">
          {formatSize(attachment.size_bytes)}
        </p>
      </div>
      <div className="flex gap-1">
        {canPreview && (
          <Dialog open={isOpen} onOpenChange={setIsOpen}>
            <DialogTrigger asChild>
              <Button variant="ghost" size="icon" className="h-8 w-8 text-muted-foreground hover:text-foreground">
                <Eye className="h-4 w-4" />
                <span className="sr-only">Preview</span>
              </Button>
            </DialogTrigger>
            <DialogContent className="max-w-4xl w-full h-[80vh] p-0 overflow-hidden">
              {isOpen && <AttachmentPreview attachment={attachment} />}
            </DialogContent>
          </Dialog>
        )}
        <Button variant="ghost" size="icon" className="h-8 w-8 text-muted-foreground hover:text-foreground" asChild>
          <a href={attachment.download_url} download={attachment.filename} target="_blank" rel="noopener noreferrer">
            <Download className="h-4 w-4" />
            <span className="sr-only">Download</span>
          </a>
        </Button>
      </div>
    </div>
  );
}
