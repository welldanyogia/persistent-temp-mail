import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import "./globals.css";
import { ThemeProvider } from "@/components/theme-provider"
import { Toaster } from "@/components/ui/sonner"
import { AuthProvider } from "@/contexts/auth-context"
import { RealtimeProvider } from "@/contexts/realtime-context"

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  metadataBase: new URL('https://webrana.id'),
  title: {
    default: 'Persistent Temp Mail',
    template: '%s | Persistent Temp Mail',
  },
  description: 'Create permanent temporary email addresses. Receive emails instantly, manage multiple aliases, and protect your privacy with our secure and persistent temp mail service.',
  keywords: ['temp mail', 'temporary email', 'disposable email', 'anonymous email', 'secure email', 'privacy', 'spam protection'],
  authors: [{ name: 'Webrana' }],
  openGraph: {
    type: 'website',
    locale: 'en_US',
    url: 'https://webrana.id',
    title: 'Persistent Temp Mail',
    description: 'Create permanent temporary email addresses. Protect your privacy with secure and persistent temp mail.',
    siteName: 'Persistent Temp Mail',
    images: [
      {
        url: 'https://webrana.id/og-image.jpg',
        width: 1200,
        height: 630,
        alt: 'Persistent Temp Mail Preview',
      },
    ],
  },
  twitter: {
    card: 'summary_large_image',
    title: 'Persistent Temp Mail',
    description: 'Create permanent temporary email addresses. Protect your privacy with secure and persistent temp mail.',
    images: ['https://webrana.id/og-image.jpg'],
    creator: '@webrana',
  },
  robots: {
    index: true,
    follow: true,
    googleBot: {
      index: true,
      follow: true,
      'max-video-preview': -1,
      'max-image-preview': 'large',
      'max-snippet': -1,
    },
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body
        className={`${geistSans.variable} ${geistMono.variable} antialiased`}
      >
        <a 
          href="#main-content" 
          className="sr-only focus:not-sr-only focus:absolute focus:top-4 focus:left-4 focus:z-50 focus:p-4 focus:bg-background focus:text-foreground focus:ring-2 focus:ring-ring focus:rounded-md"
        >
          Skip to content
        </a>
        <ThemeProvider
          attribute="class"
          defaultTheme="system"
          enableSystem
          disableTransitionOnChange
        >
          <AuthProvider>
            <RealtimeProvider>
              {children}
              <Toaster />
            </RealtimeProvider>
          </AuthProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
