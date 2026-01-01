"use client";

import { createContext, useContext, useEffect, useState } from "react";
import { useTheme as useNextTheme } from "next-themes";

type Theme = "dark" | "light" | "system";

interface ThemeContextType {
  theme: Theme;
  setTheme: (theme: Theme) => void;
}

const ThemeContext = createContext<ThemeContextType | undefined>(undefined);

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const { theme, setTheme, systemTheme } = useNextTheme();
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setMounted(true);
  }, []);

  const currentTheme = (theme === "system" ? systemTheme : theme) as Theme;

  // We actually don't need to manually apply class 'dark' to document.documentElement
  // because next-themes handles it automatically with `attribute="class"`.
  // However, we expose the resolved theme if needed.

  if (!mounted) {
    return null; // Avoid hydration mismatch
  }

  return (
    <ThemeContext.Provider value={{ theme: theme as Theme, setTheme: (t) => setTheme(t) }}>
      {children}
    </ThemeContext.Provider>
  );
}

export function useTheme() {
  const context = useContext(ThemeContext);
  if (context === undefined) {
    throw new Error("useTheme must be used within a ThemeProvider");
  }
  return context;
}
