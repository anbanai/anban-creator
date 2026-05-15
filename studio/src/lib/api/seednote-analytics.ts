import { http, unwrap } from '@/lib/http-client'
import type { SeednoteAnalytics } from '@/types/seednote-analytics'

export const seednoteAnalyticsApi = {
  getByTask: (taskId: string) =>
    unwrap<SeednoteAnalytics>(http.get(`/tasks/${taskId}/seednote-analytics`)),
}
