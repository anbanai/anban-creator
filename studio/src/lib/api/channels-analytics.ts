import { http, unwrap } from '@/lib/http-client'
import type { ChannelsAnalytics } from '@/types/channels-analytics'

export const channelsAnalyticsApi = {
  getByTask: (taskId: string) =>
    unwrap<ChannelsAnalytics>(http.get(`/tasks/${taskId}/channels-analytics`)),
  bind: (taskId: string, videoUrl: string) =>
    unwrap<{ tracking: boolean }>(http.post(`/tasks/${taskId}/channels-analytics/bind`, { video_url: videoUrl })),
}
