// Package sanitizer provides HTML sanitization for email content
// to prevent XSS attacks and block tracking pixels.
package sanitizer

import (
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// HTMLSanitizer provides methods to sanitize HTML content from emails
// Feature: email-inbox-api
// Validates: Requirements 2.8, 7.3, 7.4
type HTMLSanitizer interface {
	// Sanitize applies all sanitization rules to HTML content
	Sanitize(html string) string
	// RemoveScripts removes all script tags and their content
	RemoveScripts(html string) string
	// RemoveEventHandlers removes all inline event handlers (onclick, onload, etc.)
	RemoveEventHandlers(html string) string
	// BlockExternalImages replaces external image sources with a placeholder
	BlockExternalImages(html string) string
	// AllowInlineImages allows base64 data URI images
	AllowInlineImages(html string) string
}

// DefaultHTMLSanitizer implements HTMLSanitizer using bluemonday
type DefaultHTMLSanitizer struct {
	policy *bluemonday.Policy
}

// NewHTMLSanitizer creates a new HTML sanitizer with secure defaults
func NewHTMLSanitizer() *DefaultHTMLSanitizer {
	// Start with UGC (User Generated Content) policy as base
	policy := bluemonday.UGCPolicy()

	// Remove script-related elements
	policy.AllowElements("html", "head", "body", "title", "meta")

	// Allow common formatting elements
	policy.AllowElements(
		"p", "br", "hr", "div", "span",
		"h1", "h2", "h3", "h4", "h5", "h6",
		"strong", "b", "em", "i", "u", "s", "strike",
		"blockquote", "pre", "code",
		"ul", "ol", "li", "dl", "dt", "dd",
		"table", "thead", "tbody", "tfoot", "tr", "th", "td",
		"a", "img",
		"font", "center",
	)

	// Allow safe attributes
	policy.AllowAttrs("href").OnElements("a")
	policy.AllowAttrs("src", "alt", "width", "height").OnElements("img")
	policy.AllowAttrs("style", "class", "id").Globally()
	policy.AllowAttrs("align", "valign", "bgcolor", "color", "size", "face").Globally()
	policy.AllowAttrs("colspan", "rowspan", "border", "cellpadding", "cellspacing").OnElements("table", "td", "th")

	// Allow data URIs for inline images (base64)
	policy.AllowDataURIImages()

	return &DefaultHTMLSanitizer{
		policy: policy,
	}
}

// dataURIPlaceholderPrefix is used to temporarily replace data URIs during sanitization
const dataURIPlaceholderPrefix = "___DATA_URI_PLACEHOLDER_"

// Sanitize applies all sanitization rules to HTML content
// This is the main entry point for sanitizing email HTML
func (s *DefaultHTMLSanitizer) Sanitize(html string) string {
	if html == "" {
		return ""
	}

	// Step 1: Remove scripts first (before bluemonday processing)
	result := s.RemoveScripts(html)

	// Step 2: Remove event handlers
	result = s.RemoveEventHandlers(result)

	// Step 3: Block external images (replace with placeholder)
	result = s.BlockExternalImages(result)

	// Step 4: Preserve data URIs before bluemonday processing
	// bluemonday can be strict with data URIs containing special characters
	dataURIs := make(map[string]string)
	result = s.preserveDataURIs(result, dataURIs)

	// Step 5: Apply bluemonday policy for additional sanitization
	result = s.policy.Sanitize(result)

	// Step 6: Restore preserved data URIs
	result = s.restoreDataURIs(result, dataURIs)

	return result
}

// preserveDataURIs temporarily replaces data URIs with placeholders
func (s *DefaultHTMLSanitizer) preserveDataURIs(html string, store map[string]string) string {
	// Match data URIs in src attributes
	dataURIRegex := regexp.MustCompile(`(?i)(src\s*=\s*["'])(data:[^"']+)(["'])`)

	counter := 0
	result := dataURIRegex.ReplaceAllStringFunc(html, func(match string) string {
		submatches := dataURIRegex.FindStringSubmatch(match)
		if len(submatches) < 4 {
			return match
		}

		prefix := submatches[1]  // src="
		dataURI := submatches[2] // data:...
		suffix := submatches[3]  // "

		placeholder := dataURIPlaceholderPrefix + string(rune('A'+counter))
		store[placeholder] = dataURI
		counter++

		return prefix + placeholder + suffix
	})

	return result
}

// restoreDataURIs restores the original data URIs from placeholders
func (s *DefaultHTMLSanitizer) restoreDataURIs(html string, store map[string]string) string {
	result := html
	for placeholder, dataURI := range store {
		result = strings.ReplaceAll(result, placeholder, dataURI)
	}
	return result
}

// RemoveScripts removes all script tags and their content
func (s *DefaultHTMLSanitizer) RemoveScripts(html string) string {
	if html == "" {
		return ""
	}

	// Remove <script>...</script> tags and content (case insensitive)
	scriptRegex := regexp.MustCompile(`(?i)<script[^>]*>[\s\S]*?</script>`)
	result := scriptRegex.ReplaceAllString(html, "")

	// Remove self-closing script tags
	selfClosingScript := regexp.MustCompile(`(?i)<script[^>]*/?>`)
	result = selfClosingScript.ReplaceAllString(result, "")

	// Remove noscript tags
	noscriptRegex := regexp.MustCompile(`(?i)<noscript[^>]*>[\s\S]*?</noscript>`)
	result = noscriptRegex.ReplaceAllString(result, "")

	return result
}

// RemoveEventHandlers removes all inline event handlers
// Handles: onclick, onload, onerror, onmouseover, onfocus, onblur, etc.
func (s *DefaultHTMLSanitizer) RemoveEventHandlers(html string) string {
	if html == "" {
		return ""
	}

	// Match all on* event handlers in attributes
	// Pattern: on[a-z]+="..." or on[a-z]+='...' or on[a-z]+=...
	eventHandlerRegex := regexp.MustCompile(`(?i)\s+on[a-z]+\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]+)`)
	return eventHandlerRegex.ReplaceAllString(html, "")
}

// BlockExternalImages replaces external image sources with a placeholder
// This prevents tracking pixels and external resource loading
func (s *DefaultHTMLSanitizer) BlockExternalImages(html string) string {
	if html == "" {
		return ""
	}

	// Placeholder for blocked images
	const blockedImagePlaceholder = "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='100' height='100'%3E%3Crect fill='%23f0f0f0' width='100' height='100'/%3E%3Ctext x='50' y='55' text-anchor='middle' fill='%23999' font-size='12'%3EImage Blocked%3C/text%3E%3C/svg%3E"

	// Match img tags with src attribute
	imgRegex := regexp.MustCompile(`(?i)(<img[^>]*\s+src\s*=\s*)("[^"]*"|'[^']*')([^>]*>)`)

	result := imgRegex.ReplaceAllStringFunc(html, func(match string) string {
		// Extract the src value
		srcRegex := regexp.MustCompile(`(?i)src\s*=\s*["']([^"']*)["']`)
		srcMatch := srcRegex.FindStringSubmatch(match)

		if len(srcMatch) < 2 {
			return match
		}

		srcValue := srcMatch[1]

		// Allow data URIs (inline images)
		if strings.HasPrefix(strings.ToLower(srcValue), "data:") {
			return match
		}

		// Allow cid: references (Content-ID for embedded images)
		if strings.HasPrefix(strings.ToLower(srcValue), "cid:") {
			return match
		}

		// Block external URLs (http, https, //, etc.)
		if isExternalURL(srcValue) {
			// Replace src with placeholder
			return srcRegex.ReplaceAllString(match, `src="`+blockedImagePlaceholder+`"`)
		}

		return match
	})

	return result
}

// AllowInlineImages is a no-op since inline images (data URIs) are already allowed
// This method exists for interface completeness
func (s *DefaultHTMLSanitizer) AllowInlineImages(html string) string {
	// Data URIs are already allowed by default
	return html
}

// isExternalURL checks if a URL is external (http, https, protocol-relative)
func isExternalURL(url string) bool {
	url = strings.TrimSpace(strings.ToLower(url))

	// Protocol-relative URLs
	if strings.HasPrefix(url, "//") {
		return true
	}

	// HTTP/HTTPS URLs
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return true
	}

	// FTP URLs
	if strings.HasPrefix(url, "ftp://") {
		return true
	}

	return false
}
