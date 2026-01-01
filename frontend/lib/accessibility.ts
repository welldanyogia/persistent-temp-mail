// Accessibility Guidelines for Persistent Temp Mail

/*
  Focus Indicator Strategy:
  - We use a consistent focus ring style across the application.
  - The focus ring is high contrast and uses the primary color or ring color.
  - Applied via Tailwind's `focus-visible` utility to only show when navigating via keyboard.
*/

// Add to globals.css or a utility class
// .focus-ring {
//   @apply focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background;
// }

/*
  Semantic HTML Usage:
  - <main>: Main content area
  - <nav>: Navigation links
  - <header>: Page/Section headers
  - <footer>: Page footers
  - <article>: Self-contained content (e.g., email message)
  - <aside>: Sidebar content
  - <section>: Thematic grouping of content
*/

/*
  ARIA Labels:
  - Use `aria-label` for buttons/links that only contain icons.
  - Use `aria-describedby` for form inputs with helper text or error messages.
  - Use `role="alert"` for error messages.
  - Use `aria-live="polite"` for dynamic content updates (e.g., new email toast).
*/

export const A11Y_CONFIG = {
  skipLinkSelector: "#main-content",
  mainContentId: "main-content",
};
