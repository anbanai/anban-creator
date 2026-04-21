import { http, unwrap } from '@/lib/http-client'
import type { TimelineResponse } from '@/types'

export const timelineApi = {
  get: (from: string, to: string, filters?: {
    item_type?: string
    content_type?: string
    status?: string
    channel_id?: string
  }) =>
    unwrap<TimelineResponse>(http.get('/timeline', { params: { from, to, ...filters } })),
}
