package sanitizer

import (
	"regexp"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// Feature: email-inbox-api, Property 8: HTML Sanitization
// Validates: Requirements 2.8, 7.3, 7.4
//
// For any email with HTML content, the sanitized body_html SHALL have:
// - All script tags removed
// - All event handlers removed
// - External images blocked (replaced with placeholder or data URI)

// TestProperty8_HTMLSanitization_ScriptRemoval tests that all script tags are removed
func TestProperty8_HTMLSanitization_ScriptRemoval(t *testing.T) {
	sanitizer := NewHTMLSanitizer()

	rapid.Check(t, func(t *rapid.T) {
		// Generate random script content
		scriptContent := rapid.StringMatching(`[a-zA-Z0-9\s\(\)\{\};='"]+`).Draw(t, "scriptContent")
		beforeContent := rapid.StringMatching(`[a-zA-Z0-9\s<>]+`).Draw(t, "beforeContent")
		afterContent := rapid.StringMatching(`[a-zA-Z0-9\s<>]+`).Draw(t, "afterContent")

		// Create HTML with script tag
		htmlWithScript := beforeContent + "<script>" + scriptContent + "</script>" + afterContent

		// Sanitize
		result := sanitizer.Sanitize(htmlWithScript)

		// Verify no script tags remain
		scriptTagRegex := regexp.MustCompile(`(?i)<script`)
		if scriptTagRegex.MatchString(result) {
			t.Fatalf("Script tag found in sanitized output: %s", result)
		}

		// Verify script content is removed
		if strings.Contains(result, scriptContent) && len(scriptContent) > 5 {
			// Only fail if the script content is substantial and still present
			t.Fatalf("Script content '%s' found in sanitized output: %s", scriptContent, result)
		}
	})
}

// TestProperty8_HTMLSanitization_EventHandlerRemoval tests that all event handlers are removed
func TestProperty8_HTMLSanitization_EventHandlerRemoval(t *testing.T) {
	sanitizer := NewHTMLSanitizer()

	// Common event handlers to test
	eventHandlers := []string{
		"onclick", "onload", "onerror", "onmouseover", "onmouseout",
		"onfocus", "onblur", "onsubmit", "onchange", "onkeydown",
		"onkeyup", "onkeypress", "ondblclick", "oncontextmenu",
	}

	rapid.Check(t, func(t *rapid.T) {
		// Pick a random event handler
		handlerIdx := rapid.IntRange(0, len(eventHandlers)-1).Draw(t, "handlerIdx")
		handler := eventHandlers[handlerIdx]

		// Generate random handler value
		handlerValue := rapid.StringMatching(`[a-zA-Z0-9\(\)]+`).Draw(t, "handlerValue")

		// Create HTML with event handler
		html := `<div ` + handler + `="` + handlerValue + `">Content</div>`

		// Sanitize
		result := sanitizer.Sanitize(html)

		// Verify no event handlers remain
		eventRegex := regexp.MustCompile(`(?i)\s+on[a-z]+=`)
		if eventRegex.MatchString(result) {
			t.Fatalf("Event handler found in sanitized output: %s (original: %s)", result, html)
		}
	})
}

// TestProperty8_HTMLSanitization_ExternalImagesBlocked tests that external images are blocked
func TestProperty8_HTMLSanitization_ExternalImagesBlocked(t *testing.T) {
	sanitizer := NewHTMLSanitizer()

	rapid.Check(t, func(t *rapid.T) {
		// Generate random domain and path
		domain := rapid.StringMatching(`[a-z]+\.[a-z]+`).Draw(t, "domain")
		path := rapid.StringMatching(`/[a-z]+\.(png|jpg|gif)`).Draw(t, "path")

		// Create HTML with external image
		externalURL := "https://" + domain + path
		html := `<img src="` + externalURL + `" alt="test">`

		// Sanitize
		result := sanitizer.Sanitize(html)

		// Verify external URL is not in output
		if strings.Contains(result, externalURL) {
			t.Fatalf("External image URL found in sanitized output: %s", result)
		}

		// Verify http:// and https:// URLs are blocked
		if strings.Contains(result, "https://"+domain) || strings.Contains(result, "http://"+domain) {
			t.Fatalf("External domain found in sanitized output: %s", result)
		}
	})
}

// TestProperty8_HTMLSanitization_DataURIAllowed tests that data URI images are preserved
func TestProperty8_HTMLSanitization_DataURIAllowed(t *testing.T) {
	sanitizer := NewHTMLSanitizer()

	rapid.Check(t, func(t *rapid.T) {
		// Generate random base64-like content (simplified)
		base64Content := rapid.StringMatching(`[A-Za-z0-9+/=]{10,50}`).Draw(t, "base64Content")

		// Create HTML with data URI image
		dataURI := "data:image/png;base64," + base64Content
		html := `<img src="` + dataURI + `" alt="inline">`

		// Sanitize
		result := sanitizer.Sanitize(html)

		// Verify data URI is preserved (at least the data: prefix should remain)
		if !strings.Contains(result, "data:") {
			t.Fatalf("Data URI was removed from sanitized output: %s (original: %s)", result, html)
		}
	})
}

// TestProperty8_HTMLSanitization_CombinedThreats tests sanitization of HTML with multiple threats
func TestProperty8_HTMLSanitization_CombinedThreats(t *testing.T) {
	sanitizer := NewHTMLSanitizer()

	rapid.Check(t, func(t *rapid.T) {
		// Generate components
		scriptContent := rapid.StringMatching(`alert\(['"a-z]+['"]\)`).Draw(t, "scriptContent")
		domain := rapid.StringMatching(`[a-z]+\.[a-z]+`).Draw(t, "domain")

		// Create HTML with multiple threats
		html := `<html>
			<head><script>` + scriptContent + `</script></head>
			<body>
				<div onclick="evil()">Click me</div>
				<img src="https://` + domain + `/track.gif" onload="track()">
				<p>Safe content</p>
			</body>
		</html>`

		// Sanitize
		result := sanitizer.Sanitize(html)

		// Verify all threats are neutralized
		scriptRegex := regexp.MustCompile(`(?i)<script`)
		if scriptRegex.MatchString(result) {
			t.Fatalf("Script tag found in sanitized output")
		}

		eventRegex := regexp.MustCompile(`(?i)\s+on[a-z]+=`)
		if eventRegex.MatchString(result) {
			t.Fatalf("Event handler found in sanitized output")
		}

		if strings.Contains(result, "https://"+domain) {
			t.Fatalf("External image URL found in sanitized output")
		}

		// Verify safe content is preserved
		if !strings.Contains(result, "Safe content") {
			t.Fatalf("Safe content was removed from output")
		}
	})
}

// Unit tests for specific edge cases

func TestRemoveScripts_VariousFormats(t *testing.T) {
	sanitizer := NewHTMLSanitizer()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic script tag",
			input:    "<script>alert('xss')</script>",
			expected: "",
		},
		{
			name:     "script with attributes",
			input:    `<script type="text/javascript">alert('xss')</script>`,
			expected: "",
		},
		{
			name:     "uppercase SCRIPT",
			input:    "<SCRIPT>alert('xss')</SCRIPT>",
			expected: "",
		},
		{
			name:     "mixed case ScRiPt",
			input:    "<ScRiPt>alert('xss')</ScRiPt>",
			expected: "",
		},
		{
			name:     "script with newlines",
			input:    "<script>\nalert('xss')\n</script>",
			expected: "",
		},
		{
			name:     "noscript tag",
			input:    "<noscript>fallback</noscript>",
			expected: "",
		},
		{
			name:     "preserve other content",
			input:    "<p>Hello</p><script>evil()</script><p>World</p>",
			expected: "<p>Hello</p><p>World</p>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizer.RemoveScripts(tt.input)
			if result != tt.expected {
				t.Errorf("RemoveScripts(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestRemoveEventHandlers_VariousFormats(t *testing.T) {
	sanitizer := NewHTMLSanitizer()

	tests := []struct {
		name     string
		input    string
		contains string
		excludes string
	}{
		{
			name:     "onclick double quotes",
			input:    `<div onclick="alert('xss')">test</div>`,
			contains: "<div",
			excludes: "onclick",
		},
		{
			name:     "onclick single quotes",
			input:    `<div onclick='alert("xss")'>test</div>`,
			contains: "<div",
			excludes: "onclick",
		},
		{
			name:     "onload",
			input:    `<img src="x" onload="evil()">`,
			contains: "<img",
			excludes: "onload",
		},
		{
			name:     "onerror",
			input:    `<img src="x" onerror="evil()">`,
			contains: "<img",
			excludes: "onerror",
		},
		{
			name:     "onmouseover",
			input:    `<a onmouseover="evil()">link</a>`,
			contains: "<a",
			excludes: "onmouseover",
		},
		{
			name:     "uppercase ONCLICK",
			input:    `<div ONCLICK="evil()">test</div>`,
			contains: "<div",
			excludes: "ONCLICK",
		},
		{
			name:     "multiple handlers",
			input:    `<div onclick="a()" onmouseover="b()">test</div>`,
			contains: "<div",
			excludes: "onclick",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizer.RemoveEventHandlers(tt.input)
			if !strings.Contains(result, tt.contains) {
				t.Errorf("Result should contain %q, got %q", tt.contains, result)
			}
			if strings.Contains(strings.ToLower(result), strings.ToLower(tt.excludes)) {
				t.Errorf("Result should not contain %q, got %q", tt.excludes, result)
			}
		})
	}
}

func TestBlockExternalImages(t *testing.T) {
	sanitizer := NewHTMLSanitizer()

	tests := []struct {
		name           string
		input          string
		shouldBlock    bool
		shouldContain  string
	}{
		{
			name:          "https image",
			input:         `<img src="https://example.com/image.png">`,
			shouldBlock:   true,
			shouldContain: "Image Blocked",
		},
		{
			name:          "http image",
			input:         `<img src="http://example.com/image.png">`,
			shouldBlock:   true,
			shouldContain: "Image Blocked",
		},
		{
			name:          "protocol-relative image",
			input:         `<img src="//example.com/image.png">`,
			shouldBlock:   true,
			shouldContain: "Image Blocked",
		},
		{
			name:          "data URI image",
			input:         `<img src="data:image/png;base64,ABC123">`,
			shouldBlock:   false,
			shouldContain: "data:image/png;base64,ABC123",
		},
		{
			name:          "cid reference",
			input:         `<img src="cid:image001@example.com">`,
			shouldBlock:   false,
			shouldContain: "cid:image001@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizer.BlockExternalImages(tt.input)
			if !strings.Contains(result, tt.shouldContain) {
				t.Errorf("Result should contain %q, got %q", tt.shouldContain, result)
			}
		})
	}
}

func TestSanitize_EmptyInput(t *testing.T) {
	sanitizer := NewHTMLSanitizer()

	result := sanitizer.Sanitize("")
	if result != "" {
		t.Errorf("Sanitize empty string should return empty, got %q", result)
	}
}

func TestSanitize_SafeHTML(t *testing.T) {
	sanitizer := NewHTMLSanitizer()

	safeHTML := `<p>Hello <strong>World</strong></p><ul><li>Item 1</li><li>Item 2</li></ul>`
	result := sanitizer.Sanitize(safeHTML)

	// Safe content should be preserved
	if !strings.Contains(result, "Hello") {
		t.Error("Safe content 'Hello' was removed")
	}
	if !strings.Contains(result, "World") {
		t.Error("Safe content 'World' was removed")
	}
	if !strings.Contains(result, "Item 1") {
		t.Error("Safe content 'Item 1' was removed")
	}
}
