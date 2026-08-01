import { http, unwrap } from '@/lib/http-client'
import type { ImageCapabilityListResponse } from '@/types'

export const imageCapabilitiesApi = {
  list: () => unwrap<ImageCapabilityListResponse>(http.get('/image-capabilities')),
}
