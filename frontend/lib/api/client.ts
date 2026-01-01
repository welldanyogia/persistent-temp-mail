const API_BASE = process.env.NEXT_PUBLIC_API_URL || 'https://api.webrana.id/api/v1';

interface RequestOptions extends RequestInit {
  skipAuth?: boolean;
}

interface ApiError {
  code: string;
  message: string;
  details?: Record<string, string[]>;
}

interface ApiResponse<T> {
  success: boolean;
  data: T;
  error?: ApiError;
  timestamp: string;
}

class ApiClient {
  private token: string | null = null;
  private refreshToken_: string | null = null;
  private isRefreshing = false;
  private refreshPromise: Promise<boolean> | null = null;

  constructor() {
    if (typeof window !== 'undefined') {
      this.token = localStorage.getItem('access_token');
      this.refreshToken_ = localStorage.getItem('refresh_token');
    }
  }

  setToken(token: string | null) {
    this.token = token;
    if (typeof window !== 'undefined') {
      if (token) {
        localStorage.setItem('access_token', token);
      } else {
        localStorage.removeItem('access_token');
      }
    }
  }

  setRefreshToken(token: string | null) {
    this.refreshToken_ = token;
    if (typeof window !== 'undefined') {
      if (token) {
        localStorage.setItem('refresh_token', token);
      } else {
        localStorage.removeItem('refresh_token');
      }
    }
  }

  getRefreshToken(): string | null {
    return this.refreshToken_;
  }

  clearTokens() {
    this.setToken(null);
    this.setRefreshToken(null);
  }

  async request<T>(endpoint: string, options: RequestOptions = {}): Promise<T> {
    const { skipAuth, ...fetchOptions } = options;

    const headers: HeadersInit = {
      'Content-Type': 'application/json',
      ...fetchOptions.headers,
    };

    if (!skipAuth && this.token) {
      (headers as Record<string, string>)['Authorization'] = `Bearer ${this.token}`;
    }

    const url = endpoint.startsWith('http') ? endpoint : `${API_BASE}${endpoint}`;

    try {
      const response = await fetch(url, {
        ...fetchOptions,
        headers,
      });

      if (response.status === 401 && !skipAuth) {
        const refreshed = await this.refreshTokenRequest();
        if (refreshed) {
          (headers as Record<string, string>)['Authorization'] = `Bearer ${this.token}`;
          const retryResponse = await fetch(url, {
            ...fetchOptions,
            headers,
          });
          return this.handleResponse<T>(retryResponse);
        } else {
          this.clearTokens();
          if (typeof window !== 'undefined') {
            window.location.href = '/login';
          }
          throw new Error('Session expired. Please login again.');
        }
      }

      return this.handleResponse<T>(response);
    } catch (error: unknown) {
      if (error instanceof Error && error.message === 'Session expired. Please login again.') {
        throw error;
      }
      const msg = error instanceof Error ? error.message : 'Network request failed';
      throw new Error(msg);
    }
  }

  private async handleResponse<T>(response: Response): Promise<T> {
    let data: unknown;
    try {
      data = await response.json();
    } catch (e) {
      if (!response.ok) {
        throw new Error(`Request failed with status ${response.status}`);
      }
      return {} as T;
    }

    if (!response.ok) {
      const errorData = data as { error?: { message?: string } | string; message?: string };
      let errorMessage = `Request failed: ${response.statusText}`;

      if (errorData?.error) {
        if (typeof errorData.error === 'string') {
          errorMessage = errorData.error;
        } else if (typeof errorData.error === 'object' && errorData.error.message) {
          errorMessage = errorData.error.message;
        }
      } else if (errorData?.message) {
        errorMessage = errorData.message;
      }

      throw new Error(errorMessage);
    }

    if (this.isApiResponse<T>(data)) {
      return data.data;
    }

    return data as T;
  }

  private isApiResponse<T>(data: unknown): data is ApiResponse<T> {
    return (
      typeof data === 'object' &&
      data !== null &&
      'success' in data &&
      'data' in data
    );
  }

  private async refreshTokenRequest(): Promise<boolean> {
    if (this.isRefreshing) {
      return this.refreshPromise || Promise.resolve(false);
    }

    // No refresh token available
    if (!this.refreshToken_) {
      return false;
    }

    this.isRefreshing = true;

    this.refreshPromise = (async () => {
      try {
        const response = await fetch(`${API_BASE}/auth/refresh`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({ refresh_token: this.refreshToken_ }),
        });

        if (response.ok) {
          const data = await response.json();
          // Contract: data.data.tokens.access_token and data.data.tokens.refresh_token
          const tokens = data.data?.tokens;

          if (tokens?.access_token) {
            this.setToken(tokens.access_token);
            // Also update refresh token if a new one is provided
            if (tokens.refresh_token) {
              this.setRefreshToken(tokens.refresh_token);
            }
            return true;
          }
        }
        return false;
      } catch (err) {
        console.error('Token refresh error:', err);
        return false;
      } finally {
        this.isRefreshing = false;
        this.refreshPromise = null;
      }
    })();

    return this.refreshPromise;
  }

  get<T>(endpoint: string, options?: RequestOptions) {
    return this.request<T>(endpoint, { ...options, method: 'GET' });
  }

  post<T>(endpoint: string, body: unknown, options?: RequestOptions) {
    return this.request<T>(endpoint, {
      ...options,
      method: 'POST',
      body: JSON.stringify(body),
    });
  }

  put<T>(endpoint: string, body: unknown, options?: RequestOptions) {
    return this.request<T>(endpoint, {
      ...options,
      method: 'PUT',
      body: JSON.stringify(body),
    });
  }

  patch<T>(endpoint: string, body: unknown, options?: RequestOptions) {
    return this.request<T>(endpoint, {
      ...options,
      method: 'PATCH',
      body: JSON.stringify(body),
    });
  }

  delete<T>(endpoint: string, options?: RequestOptions) {
    return this.request<T>(endpoint, { ...options, method: 'DELETE' });
  }
}

export const apiClient = new ApiClient();
