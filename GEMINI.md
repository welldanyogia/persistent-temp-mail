GEMINI.md - Frontend Context & Instructions

  1. Project Identity
  Name: Persistent Temp Mail (Frontend)
  Type: Next.js 16 Web Application
  Core Goal: Menyediakan antarmuka modern, responsif, dan aman untuk layanan email sementara yang persisten (bukan
  disposable sekali pakai).

  2. Technology Stack & Constraints
  Kamu WAJIB mematuhi stack berikut. Jangan menyarankan library di luar list ini tanpa alasan krusial.

   * Framework: Next.js 16 (App Router app/ directory).
   * Language: TypeScript (Strict Mode). DILARANG menggunakan any secara eksplisit maupun implisit.
   * UI Library: React 19.
   * Styling: Tailwind CSS v4.
   * Component System: Shadcn UI (Radix UI primitives).
   * State Management: React Context (Global), React Server Components (Data Fetching), React Hooks (Local).
   * Real-time: Server-Sent Events (Native EventSource).
   * Testing: Vitest (Unit/Component), Playwright (E2E).
   * Package Manager: npm.

  3. Architecture & Directory Structure
  Structure project mengikuti pola Next.js App Router standar dengan pemisahan logic yang jelas.

    1 frontend/
    2 ├── app/                    # Routes (Server Components by default)
    3 │   ├── (auth)/             # Login/Register routes group
    4 │   ├── (dashboard)/        # Protected routes (Inbox, Settings, Domains)
    5 │   └── layout.tsx          # Root layout
    6 ├── components/
    7 │   ├── ui/                 # Shadcn primitives (Button, Card, etc.)
    8 │   ├── email/              # Email specific components (List, Viewer)
    9 │   ├── alias/              # Alias management components
    10 │   ├── domain/             # Domain management components
    11 │   └── shared/             # Generic shared components
    12 ├── lib/
    13 │   ├── api/                # API Client & Service modules
    14 │   │   ├── client.ts       # Base HTTP Client (fetch wrapper)
    15 │   │   └── *.ts            # Domain specific services (auth.ts, emails.ts)
    16 │   ├── hooks/              # Custom React Hooks
    17 │   └── utils.ts            # Helper functions
    18 ├── types/                  # TypeScript Interfaces (Wajib sinkron dengan API Contract)
    19 └── __tests__/              # Test files

  4. Coding Standards & Conventions

  4.1. TypeScript Rules
   * Gunakan Interface untuk definisi object/model data.
   * Gunakan Type untuk union types atau primitives.
   * Strict Props typing untuk semua React Components.
   * Hindari as casting sebisa mungkin. Gunakan Type Guards.

  4.2. Component Patterns
   * Server Components: Gunakan untuk data fetching awal (SEO & Performance).
   * Client Components: Gunakan 'use client' hanya pada komponen yang membutuhkan interaktivitas (State, Effects, Event
     Listeners).
   * Composition: Pecah komponen besar menjadi komponen kecil yang fokus (Single Responsibility).

  4.3. API Integration
  Semua interaksi API harus melalui lib/api/.
   * Base URL: https://api.webrana.id/v1 (Production) / http://localhost:8080/v1 (Dev).
   * Authentication: JWT Bearer Token di header Authorization.
   * Response Handling:
       * Wrap semua call dengan ApiClient.
       * Handle standard response envelope:
   1         interface ApiResponse<T> {
   2           success: boolean;
   3           data: T;
   4           error?: { code: string; message: string; };
   5         }
   * Data Models: Rujuk ke types/ yang harus selalu sinkron dengan docs/api-contracts.md.

  4.4. Real-time (SSE)
   * Gunakan endpoint GET /api/v1/events.
   * Implementasi auto-reconnection dengan exponential backoff.
   * Handle event types: new_email, email_deleted, alias_created, domain_verified.

  5. Security Guidelines (Frontend)
   * XSS Prevention: Gunakan library sanitasi (DOMPurify) saat me-render HTML email (body_html).
   * External Images: Block gambar eksternal secara default. Berikan tombol "Load Images" user-initiated.
   * Auth: Token disimpan di localStorage (sesuai arsitektur saat ini) dan di-handle oleh AuthContext.
   * Route Protection: Middleware/Layout checks untuk redirect user yang belum login dari protected routes.

  6. Git Workflow & Commits
  Gunakan format Conventional Commits:
   * feat(scope): description
   * fix(scope): description
   * refactor(scope): description
   * docs(scope): description

  Contoh: feat(inbox): implement bulk delete functionality

  7. Knowledge Base References
  Gunakan file-file berikut sebagai "Source of Truth":

   * API Contract: @docs/api-contracts.md & @docs/api-specification.md (Struktur Request/Response, Endpoint, Error
     Codes).
   * Security: @docs/security-guidelines.md (Sanitasi HTML, Validasi Input).
   * Design/UI: @.kiro/specs/frontend-ui/design.md (Komponen visual, flow user).
   * Testing: @docs/testing-strategy.md (Cara menulis test yang valid).

  8. Specific Task Instructions (System Prompts)

  Saat saya meminta bantuanmu untuk coding:
   1. Analisis Dulu: Cek file yang relevan menggunakan read_file atau search_file_content sebelum menulis kode.
   2. No Hallucinations: Jangan mengarang endpoint API. Selalu cek api-contracts.md.
   3. Type Safety: Pastikan semua kode baru ter-type dengan benar. Jika tipe belum ada, buat di folder types/.
   4. Idiomatic Next.js: Gunakan fitur Next.js 16 dengan benar (misal: next/image, next/link, Server Actions jika
      relevan).
   5. Clean Code: Kode harus bersih, terformat (Prettier), dan minim komentar yang tidak perlu.

  ---