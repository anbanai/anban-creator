import { http, unwrap } from '@/lib/http-client'
import type { WechatAnalytics } from '@/types/wechat-analytics'

export const wechatAnalyticsApi = {
  getByTask: (taskId: string) =>
    unwrap<WechatAnalytics>(http.get(`/tasks/${taskId}/wechat-analytics`)),
  bind: (taskId: string, articleUrl: string) =>
    unwrap<{ tracking: boolean }>(http.post(`/tasks/${taskId}/wechat-analytics/bind`, { article_url: articleUrl })),
}
