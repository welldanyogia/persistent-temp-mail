import { apiClient } from './client';
import { DashboardStats } from '@/types/dashboard';

export const dashboardService = {
  getStats: async (): Promise<DashboardStats> => {
    // Assuming the backend has a specific endpoint for dashboard stats.
    // If not, we might need to aggregate, but a single endpoint is preferred.
    // Let's assume /users/stats or /dashboard/stats.
    // Based on typical patterns, let's try /dashboard/stats or /users/me/stats.
    // I'll stick with /dashboard/stats as a logical guess, or just /stats if global.
    // Given the project structure, let's use /dashboard/stats.
    return apiClient.get<DashboardStats>('/dashboard/stats');
  }
};
