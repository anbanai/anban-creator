import { http, unwrap } from '@/lib/http-client'

export interface ResolveDownloadUrlRequest {
  upload_id?: string
  key: string
  owner_type?: 'task' | 'plan'
  owner_id?: string
}

export interface ResolveDownloadUrlResponse {
  url: string
  expires_at: string
}

export const uploadsApi = {
  resolveDownloadUrl: (payload: ResolveDownloadUrlRequest) =>
    unwrap<ResolveDownloadUrlResponse>(http.post('/uploads/resolve-download-url', payload)),
}
