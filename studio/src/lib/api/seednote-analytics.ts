import { http, unwrap } from '@/lib/http-client'
import type { SeednoteAnalytics } from '@/types/seednote-analytics'

export const seednoteAnalyticsApi = {
  getByTask: (taskId: string) =>
    unwrap<SeednoteAnalytics>(http.get(`/tasks/${taskId}/seednote-analytics`)),
  bind: (taskId: string, identity: { note_id?: string; note_url?: string }) =>
    unwrap<{ tracking: boolean }>(http.post(`/tasks/${taskId}/seednote-analytics/bind`, identity)),
}
