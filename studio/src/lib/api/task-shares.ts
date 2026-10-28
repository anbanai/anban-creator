import { http, unwrap } from '@/lib/http-client'
import type { PublicShare, TaskShareStatus } from '@/types/task-share'

export const taskSharesApi = {
  get: (taskId: string) => unwrap<TaskShareStatus>(http.get(`/tasks/${encodeURIComponent(taskId)}/share`, { headers: { 'Cache-Control': 'no-cache' } })),
  save: (taskId: string, days: number) => unwrap<TaskShareStatus>(http.put(`/tasks/${encodeURIComponent(taskId)}/share`, { expires_in_days: days })),
  revoke: async (taskId: string) => { await http.delete(`/tasks/${encodeURIComponent(taskId)}/share`) },
}

export class ShareReadError extends Error {
  constructor(public status: number) { super('分享读取失败') }
}

function apiBase() {
  return new URL(import.meta.env.VITE_API_BASE_URL || '/api/v1', window.location.origin)
}

export function shareImageURL(path: string, token: string): string | undefined {
  const base = apiBase()
  const url = new URL(path, base.origin)
  const prefix = `/api/v1/public/shares/${encodeURIComponent(token)}/images/`
  if (url.origin !== base.origin || !url.pathname.startsWith(prefix) || !url.pathname.slice(prefix.length) || url.search || url.hash) return undefined
  return url.href
}

// Public reads never use the authenticated client or its refresh interceptors.
export async function readPublicShare(token: string, signal: AbortSignal): Promise<PublicShare> {
  const base = apiBase().href.replace(/\/$/, '')
  const response = await fetch(`${base}/public/shares/${encodeURIComponent(token)}`, { cache: 'no-store', credentials: 'omit', referrerPolicy: 'no-referrer', signal })
  if (!response.ok) throw new ShareReadError(response.status)
  const envelope = await response.json()
  return envelope.data as PublicShare
}
