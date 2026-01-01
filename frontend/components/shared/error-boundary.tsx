"use client";

import React, { Component, ErrorInfo, ReactNode } from "react";
import { EmptyState } from "./empty-state";
import { Button } from "@/components/ui/button";

interface Props {
  children?: ReactNode;
  fallback?: ReactNode;
}

interface State {
  hasError: boolean;
  error: Error | null;
}

export class ErrorBoundary extends Component<Props, State> {
  public state: State = {
    hasError: false,
    error: null,
  };

  public static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  public componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error("Uncaught error:", error, errorInfo);
  }

  public render() {
    if (this.state.hasError) {
      if (this.props.fallback) {
        return this.props.fallback;
      }

      return (
        <div className="flex min-h-[400px] w-full items-center justify-center p-4">
          <EmptyState
            icon="error"
            title="Something went wrong"
            description="We encountered an unexpected error. Please try again."
            action={{
              label: "Try again",
              onClick: () => this.setState({ hasError: false, error: null }),
            }}
          />
        </div>
      );
    }

    return this.props.children;
  }
}
