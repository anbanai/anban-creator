import { http, unwrap } from '@/lib/http-client'
import type { VideoEstimateRequest, VideoEstimateResponse, VideoModelSpec } from '@/types'

export const videoApi = {
  models: () =>
    unwrap<{ items: VideoModelSpec[] }>(http.get('/video/models')),

  estimate: (data: VideoEstimateRequest) =>
    unwrap<VideoEstimateResponse>(http.post('/video/estimate', data)),
}
