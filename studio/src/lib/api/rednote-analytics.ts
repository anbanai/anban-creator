import { http, unwrap } from '@/lib/http-client'
import type { RednoteAnalytics } from '@/types/rednote-analytics'

export const rednoteAnalyticsApi = {
  getByTask: (taskId: string) =>
    unwrap<RednoteAnalytics>(http.get(`/tasks/${taskId}/rednote-analytics`)),
}
