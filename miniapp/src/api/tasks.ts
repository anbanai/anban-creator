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

  list: (params?: { status?: string; project_id?: string; limit?: number; offset?: number }) =>
    get<PaginatedResponse<Task>>('/tasks', params as Record<string, any>),

  get: (id: string) =>
    get<Task>(`/tasks/${id}`),

  cancel: (id: string) =>
    post<void>(`/tasks/${id}/cancel`),

  /** Retry a failed/cancelled task by cloning its full configuration
   *  (three-dimensional style, author/persona, ecommerce config, image model,
   *  watermark, goal mode...) into a fresh billed task on the server. */
  retry: (id: string) =>
    post<Task>(`/tasks/${id}/retry`),

  delete: (id: string) =>
    del<void>(`/tasks/${id}`),

  markPublished: (id: string, published: boolean) =>
    patch<void>(`/tasks/${id}/published`, { published }),

  getFiles: (id: string) =>
    get<TaskFile[]>(`/tasks/${id}/files`),

  /** Fetch the rendered WeChat HTML preview for an article task.
   *  The /preview endpoint returns raw text/html (not the JSON envelope),
   *  so it bypasses the standard `get` helper. */
  fetchPreviewHTML: (id: string): Promise<string> =>
    new Promise((resolve, reject) => {
      uni.request({
        url: `${API_BASE_URL}/tasks/${id}/preview`,
        method: 'GET',
        responseType: 'text' as any,
        dataType: '' as any,
        header: tasksApi.downloadHeaders(),
        success(res) {
          if (res.statusCode === 200) {
            resolve(typeof res.data === 'string' ? res.data : String(res.data ?? ''))
          } else {
            reject(new Error(`预览失败 (${res.statusCode})`))
          }
        },
        fail(err) {
          reject(new Error(err.errMsg || '预览失败'))
        },
      })
    }),

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

  /** Download a single task's files as a ZIP via uni.downloadFile (GET endpoint).
   *  Mirrors the detail page's downloadZip flow. Resolves with the saved file path
   *  after persisting to the user data dir. Used by the list page for both single
   *  and bulk (sequential) downloads — uni.downloadFile only supports GET, so the
   *  POST bulk-zip endpoint is approximated by iterating per-task. */
  downloadTaskZip: (taskId: string) =>
    new Promise<string>((resolve, reject) => {
      uni.downloadFile({
        url: tasksApi.zipDownloadUrl(taskId),
        header: tasksApi.downloadHeaders(),
        success(res) {
          if (res.statusCode !== 200) {
            reject(new Error(`下载失败 (${res.statusCode})`))
            return
          }
          uni.saveFile({
            tempFilePath: res.tempFilePath,
            success(saved) {
              resolve(saved.savedFilePath)
            },
            fail() {
              // Save can fail on some platforms; the temp path is still usable
              resolve(res.tempFilePath)
            },
          })
        },
        fail(err) {
          reject(new Error(err.errMsg || '下载失败'))
        },
      })
    }),
}
