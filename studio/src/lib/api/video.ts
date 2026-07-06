import { http, unwrap } from '@/lib/http-client'
import type { VideoEstimateRequest, VideoEstimateResponse, VideoModelSpec, VideoPlaybookSpec } from '@/types'

export const videoApi = {
  models: () =>
    unwrap<{ items: VideoModelSpec[] }>(http.get('/video/models')),

  playbooks: () =>
    unwrap<{ items: VideoPlaybookSpec[] }>(http.get('/video/playbooks')),

  estimate: (data: VideoEstimateRequest) =>
    unwrap<VideoEstimateResponse>(http.post('/video/estimate', data)),
}
