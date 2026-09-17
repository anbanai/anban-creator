import { http, unwrap } from '@/lib/http-client'
import type { MontageCapabilityListResponse } from '@/types'

export const montageCapabilitiesApi = {
  list: () => unwrap<MontageCapabilityListResponse>(http.get('/montage-capabilities')),
}
