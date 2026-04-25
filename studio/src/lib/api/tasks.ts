import { http, unwrap } from '@/lib/http-client'
import type { Task, TaskFile, CreateTaskRequest, PaginatedResponse } from '@/types'

export const tasksApi = {
  create: async (data: CreateTaskRequest): Promise<Task> => {
    const result = await unwrap<Task | Task[]>(http.post('/tasks', data))
    return Array.isArray(result) ? result[0] : result
  },

  list: (params?: { offset?: number; limit?: number; status?: string; channel_id?: string }) =>
    unwrap<PaginatedResponse<Task>>(http.get('/tasks', { params })),

  get: (id: string) =>
    unwrap<Task>(http.get(`/tasks/${id}`)),

  cancel: (id: string) =>
    unwrap<void>(http.post(`/tasks/${id}/cancel`)),

  markPublished: (id: string, published: boolean) =>
    unwrap<{ published: boolean }>(http.patch(`/tasks/${id}/published`, { published })),

  files: (id: string) =>
    unwrap<TaskFile[]>(http.get(`/tasks/${id}/files`)),

  streamUrl: (id: string) => `${import.meta.env.VITE_API_BASE_URL || '/api/v1'}/tasks/${id}/stream`,

  downloadFileBlob: async (taskId: string, fileId: string): Promise<Blob> => {
    const response = await http.get(`/tasks/${taskId}/files/${fileId}/download`, {
      responseType: 'blob',
    })
    return response.data
  },

  downloadZipBlob: async (taskId: string): Promise<Blob> => {
    const response = await http.get(`/tasks/${taskId}/files/zip`, {
      responseType: 'blob',
    })
    return response.data
  },

  fetchPreviewHTML: async (taskId: string): Promise<string> => {
    const response = await http.get(`/tasks/${taskId}/preview`, {
      responseType: 'text',
    })
    return response.data
  },
}
