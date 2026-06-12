import { get, post, patch, del } from './request'
import type {
  Task,
  TaskFile,
  CreateTaskRequest,
  PaginatedResponse,
  SeednoteAnalytics,
} from '@/types'
import { API_BASE_URL, TOKEN_KEY } from '@/utils/constants'

export const tasksApi = {
  create: async (data: CreateTaskRequest): Promise<Task> => {
    const result = await post<Task | Task[]>('/tasks', data)
    return Array.isArray(result) ? result[0] : result
  },

  list: (params?: { status?: string; channel_id?: string; limit?: number; offset?: number }) =>
    get<PaginatedResponse<Task>>('/tasks', params as Record<string, any>),

  get: (id: string) =>
    get<Task>(`/tasks/${id}`),

  cancel: (id: string) =>
    post<void>(`/tasks/${id}/cancel`),

  delete: (id: string) =>
    del<void>(`/tasks/${id}`),

  markPublished: (id: string, published: boolean) =>
    patch<void>(`/tasks/${id}/published`, { published }),

  getFiles: (id: string) =>
    get<TaskFile[]>(`/tasks/${id}/files`),

  getSeednoteAnalytics: (id: string) =>
    get<SeednoteAnalytics>(`/tasks/${id}/seednote-analytics`),

  /** Request a zip download URL for multiple tasks */
  downloadZip: (taskIds: string[]) =>
    post<{ url: string }>('/tasks/files/zip', { task_ids: taskIds }),

  fileDownloadUrl: (taskId: string, fileId: string) =>
    `${API_BASE_URL}/tasks/${taskId}/files/${fileId}/download`,

  zipDownloadUrl: (taskId: string) =>
    `${API_BASE_URL}/tasks/${taskId}/files/zip`,

  downloadHeaders: () => {
    const token = uni.getStorageSync(TOKEN_KEY)
    return token ? { Authorization: `Bearer ${token}` } : undefined
  },
}
