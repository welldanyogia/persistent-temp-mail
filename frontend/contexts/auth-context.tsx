"use client";

import React, { createContext, useContext, useEffect, useState, useCallback } from "react";
import { User, LoginRequest, RegisterRequest } from "@/types/auth";
import { authService } from "@/lib/api/auth";
import { apiClient } from "@/lib/api/client";
import { useRouter } from "next/navigation";
import { toast } from "sonner";

interface AuthContextType {
  user: User | null;
  isLoading: boolean;
  isAuthenticated: boolean;
  login: (data: LoginRequest) => Promise<void>;
  register: (data: RegisterRequest) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const router = useRouter();

  // Load user from API on mount
  const loadUser = useCallback(async () => {
    // If we have a token in local storage, try to fetch the user
    // Note: apiClient constructor checks localStorage, but we need to ensure it's synced
    // Actually apiClient.setToken handles localStorage updates, and constructor reads it.
    // So if there's a token, we try to fetch 'me'.
    
    // Optimistic check: if no token in localStorage, skip request
    const token = typeof window !== 'undefined' ? localStorage.getItem('access_token') : null;
    if (!token) {
      setUser(null);
      setIsLoading(false);
      return;
    }

    try {
      const userData = await authService.me();
      setUser(userData);
    } catch (error) {
      console.error("Failed to load user:", error);
      setUser(null);
      // If 'me' fails (e.g. 401), apiClient might have already cleared token if refresh failed
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    loadUser();
  }, [loadUser]);

  // Periodic token refresh (every 10 minutes)
  useEffect(() => {
    if (!user) return;

    const refreshInterval = setInterval(async () => {
      // We rely on authService.refresh or just let apiClient handle it on 401.
      // However, proactive refresh is good.
      // We can just call the refresh endpoint.
      try {
        await authService.refresh();
        // If refresh returns new token, it's set in apiClient
      } catch (error) {
        console.error("Token refresh failed:", error);
      }
    }, 10 * 60 * 1000); // 10 minutes

    return () => clearInterval(refreshInterval);
  }, [user]);

  const login = async (data: LoginRequest) => {
    try {
      const authData = await authService.login(data);
      apiClient.setToken(authData.tokens.access_token);
      setUser(authData.user);
      toast.success("Login successful");
      router.push("/dashboard");
    } catch (error: any) {
      toast.error(error.message || "Login failed");
      throw error;
    }
  };

  const register = async (data: RegisterRequest) => {
    try {
      const authData = await authService.register(data);
      apiClient.setToken(authData.tokens.access_token);
      setUser(authData.user);
      toast.success("Registration successful");
      router.push("/dashboard");
    } catch (error: any) {
      toast.error(error.message || "Registration failed");
      throw error;
    }
  };

  const logout = async () => {
    try {
      await authService.logout();
      setUser(null);
      toast.success("Logged out successfully");
      router.push("/login");
    } catch (error: any) {
      console.error("Logout error:", error);
      // Force local logout anyway
      apiClient.setToken(null);
      setUser(null);
      router.push("/login");
    }
  };

  return (
    <AuthContext.Provider
      value={{
        user,
        isLoading,
        isAuthenticated: !!user,
        login,
        register,
        logout,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return context;
}
