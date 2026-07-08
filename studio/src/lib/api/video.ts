import { http, unwrap } from '@/lib/http-client'
import type { VideoEstimateRequest, VideoEstimateResponse, VideoModelSpec, VideoPlaybookSpec } from '@/types'

export const videoCreatorApi = {
  models: () =>
    unwrap<{ items: VideoModelSpec[] }>(http.get('/videocreator/models')),

  playbooks: () =>
    unwrap<{ items: VideoPlaybookSpec[] }>(http.get('/videocreator/playbooks')),

  estimate: (data: VideoEstimateRequest) =>
    unwrap<VideoEstimateResponse>(http.post('/videocreator/estimate', data)),
}
