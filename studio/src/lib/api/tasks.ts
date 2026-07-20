import { http, unwrap } from '@/lib/http-client'
import type { Task, TaskFile, CreateTaskRequest, PaginatedResponse, BulkTasksResponse, VideoProductionResponse, InputAttachment } from '@/types'

export interface ResumeTaskRequest {
  prompt?: string
  input_attachments: InputAttachment[]
}

export interface CloneTaskRequest {
  prompt: string
  input_attachments: InputAttachment[]
}

export const tasksApi = {
  create: async (data: CreateTaskRequest): Promise<Task> => {
    const result = await unwrap<Task | Task[]>(http.post('/tasks', data))
    return Array.isArray(result) ? result[0] : result
  },

  list: (params?: { offset?: number; limit?: number; status?: string; project_id?: string; plan_id?: string }) =>
    unwrap<PaginatedResponse<Task>>(http.get('/tasks', { params })),

  get: (id: string) =>
    unwrap<Task>(http.get(`/tasks/${id}`)),

  cancel: (id: string) =>
    unwrap<void>(http.post(`/tasks/${id}/cancel`)),

  // Clone a completed/failed/cancelled task into a fresh billed task while
  // preserving the full frozen configuration. Returns the new task.
  clone: (id: string, data: CloneTaskRequest) =>
    unwrap<Task>(http.post(`/tasks/${id}/clone`, data)),

  resume: (id: string, data: ResumeTaskRequest) =>
    unwrap<Task>(http.post(`/tasks/${id}/resume`, {
      prompt: data.prompt?.trim() ?? '',
      input_attachments: data.input_attachments,
    })),

  delete: (id: string) =>
    unwrap<void>(http.delete(`/tasks/${id}`)),

  // Bulk operations — best-effort, ≤100 ids; the server returns a per-task
  // summary (succeeded/skipped + reasons). Each action only sends the subset it
  // can act on (cancel: pending/running; clone: failed/cancelled; delete:
  // non-running), so the count the user sees equals what is actually submitted.
  bulkCancel: (ids: string[]) =>
    unwrap<BulkTasksResponse>(http.post('/tasks/bulk-cancel', { task_ids: ids })),
  bulkClone: (ids: string[]) =>
    unwrap<BulkTasksResponse>(http.post('/tasks/bulk-clone', { task_ids: ids })),
  bulkDelete: (ids: string[]) =>
    unwrap<BulkTasksResponse>(http.post('/tasks/bulk-delete', { task_ids: ids })),

  markPublished: (id: string, published: boolean) =>
    unwrap<{ published: boolean }>(http.patch(`/tasks/${id}/published`, { published })),

  // Publish-approval gate (Batch 4A): resume a held publish (publishes the
  // frozen draft to the WeChat draft box asynchronously) or close the gate
  // without publishing. Only valid while publish_approval_state === 'pending'.
  publishApprove: (id: string) =>
    unwrap<{ approved: boolean }>(http.post(`/tasks/${id}/publish-approve`)),
  publishReject: (id: string, reason?: string) =>
    unwrap<{ rejected: boolean }>(http.post(`/tasks/${id}/publish-reject`, { reason })),

  files: (id: string) =>
    unwrap<TaskFile[]>(http.get(`/tasks/${id}/files`)),

  videoProduction: (id: string) =>
    unwrap<VideoProductionResponse>(http.get(`/tasks/${id}/video-production`)),

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

  downloadBulkZipBlob: async (taskIds: string[]): Promise<Blob> => {
    const response = await http.post('/tasks/files/zip', { task_ids: taskIds }, {
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
