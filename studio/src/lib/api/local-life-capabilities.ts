import { http, unwrap } from '@/lib/http-client'
import type { LocalLifeCapabilityListResponse } from '@/types'

export const localLifeCapabilitiesApi = {
  list: () => unwrap<LocalLifeCapabilityListResponse>(http.get('/local-life-capabilities')),
}
