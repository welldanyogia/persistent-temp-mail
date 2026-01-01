import DOMPurify from 'dompurify';

interface SanitizeOptions {
  allowExternalImages?: boolean;
}

const ALLOWED_TAGS = [
  'p', 'div', 'span', 'br', 'hr',
  'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
  'strong', 'b', 'em', 'i', 'u', 's',
  'a', 'img',
  'table', 'thead', 'tbody', 'tr', 'th', 'td',
  'ul', 'ol', 'li',
  'blockquote', 'pre', 'code',
];

const ALLOWED_ATTR = [
  'href', 'src', 'alt', 'title', 'class', 'id',
  'width', 'height', 'style',
  'colspan', 'rowspan',
  'target', 'rel',
];

const FORBID_TAGS = [
  'script', 'style', 'iframe', 'object', 'embed',
  'form', 'input', 'button', 'select', 'textarea',
  'meta', 'link', 'base',
];

const FORBID_ATTR = [
  'onerror', 'onload', 'onclick', 'onmouseover',
  'onfocus', 'onblur', 'onsubmit', 'onchange',
  'onkeydown', 'onkeyup', 'onkeypress',
];

export function sanitizeHtml(html: string, options: SanitizeOptions = {}): string {
  if (!html) return '';

  // Configure DOMPurify
  const config: DOMPurify.Config = {
    ALLOWED_TAGS,
    ALLOWED_ATTR,
    FORBID_TAGS,
    FORBID_ATTR,
    ALLOW_DATA_ATTR: false,
    ADD_ATTR: ['target'],
    WHOLE_DOCUMENT: false,
    RETURN_DOM: false,
    RETURN_DOM_FRAGMENT: false,
  };

  // Add hook to handle images
  DOMPurify.addHook('afterSanitizeAttributes', (node) => {
    // Handle links - open in new tab
    if (node.tagName === 'A') {
      node.setAttribute('target', '_blank');
      node.setAttribute('rel', 'noopener noreferrer');
    }

    // Handle images
    if (node.tagName === 'IMG') {
      const src = node.getAttribute('src') || '';

      // Allow data: URLs for inline images (data:image/...)
      if (src.startsWith('data:image/')) {
        return;
      }

      // Block or allow external images
      if (src.startsWith('http://') || src.startsWith('https://')) {
        if (!options.allowExternalImages) {
          // Replace with placeholder or style it as blocked
          // We'll replace src with a placeholder data URI or a local asset path if available.
          // For now, let's use a simple SVG placeholder data URI
          const placeholder = "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='100' height='100' viewBox='0 0 24 24' fill='none' stroke='%2394a3b8' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3E%3Crect x='3' y='3' width='18' height='18' rx='2' ry='2'/%3E%3Ccircle cx='8.5' cy='8.5' r='1.5'/%3E%3Cpolyline points='21 15 16 10 5 21'/%3E%3Cline x1='3' y1='3' x2='21' y2='21' stroke='%23ef4444' stroke-width='2'/%3E%3C/svg%3E";
          
          node.setAttribute('src', placeholder);
          node.setAttribute('alt', 'External image blocked');
          node.setAttribute('class', 'blocked-image opacity-50 border rounded bg-muted');
          node.setAttribute('data-original-src', src); // Store original src if we want to enable it later via complex DOM manipulation (not easily done via string return but useful for debugging)
        }
      }

      // Block javascript: and other dangerous protocols
      if (src.startsWith('javascript:') || src.startsWith('vbscript:')) {
        node.removeAttribute('src');
      }
    }
  });

  // Sanitize and return
  const sanitized = DOMPurify.sanitize(html, config as any);

  // Remove hooks to prevent memory leaks (DOMPurify hooks are global)
  DOMPurify.removeHook('afterSanitizeAttributes');

  return sanitized as unknown as string;
}
