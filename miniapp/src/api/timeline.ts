import { get } from './index'
import type { TimelineResponse } from '@/types'

export const timelineApi = {
  get: (params: {
    from: string
    to: string
    type?: string
    content_type?: string
    status?: string
    channel_id?: string
  }) =>
    get<TimelineResponse>('/timeline', params as Record<string, any>),
}
