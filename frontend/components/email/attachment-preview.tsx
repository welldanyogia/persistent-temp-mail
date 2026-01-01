import { Attachment } from '@/types/email';

export function AttachmentPreview({ attachment }: { attachment: Attachment }) {
  const isImage = attachment.content_type.startsWith('image/');
  const isPdf = attachment.content_type === 'application/pdf';

  if (isImage) {
    return (
      <div className="w-full h-full flex items-center justify-center bg-black/5">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={`${attachment.download_url}?inline=true`}
          alt={attachment.filename}
          className="max-h-full max-w-full object-contain"
        />
      </div>
    );
  }

  if (isPdf) {
    return (
      <iframe
        src={`${attachment.download_url}?inline=true`}
        className="w-full h-full border-none"
        title={attachment.filename}
      />
    );
  }

  return null;
}
