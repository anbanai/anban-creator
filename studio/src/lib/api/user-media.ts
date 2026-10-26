import { http, unwrap } from '@/lib/http-client'

export type UserMediaStatus = 'queued' | 'analyzing' | 'available' | 'failed'

export interface UserMediaAsset {
  id: string
  option_label?: string
  asset_id: string
  file_name: string
  content_type: string
  size: number
  media_type: 'image' | 'video'
  project_id?: string
  status: UserMediaStatus
  description?: string
  recommended_uses?: string[]
  usage_notes?: string
  error_code?: string
  error_message?: string
  preview_url?: string
  preview_expires_at?: string
  created_at: string
  updated_at: string
}

export interface UserMediaList {
  items: UserMediaAsset[]
  total: number
  offset: number
  limit: number
}

export const userMediaApi = {
  list: (params?: { q?: string; media_type?: string; project_id?: string; status?: string; offset?: number; limit?: number }) =>
    unwrap<UserMediaList>(http.get('/media-assets', { params })),
  get: (id: string) => unwrap<UserMediaAsset>(http.get(`/media-assets/${id}`)),
  create: (data: { upload_session_id: string; project_id?: string; usage_notes?: string }) =>
    unwrap<UserMediaAsset>(http.post('/media-assets', data)),
  update: (id: string, data: { project_id?: string | null; usage_notes?: string }) =>
    unwrap<UserMediaAsset>(http.patch(`/media-assets/${id}`, data)),
  reanalyze: (id: string) => unwrap<UserMediaAsset>(http.post(`/media-assets/${id}/reanalyze`)),
  remove: (id: string) => unwrap<{ removed: boolean }>(http.delete(`/media-assets/${id}`)),
}
