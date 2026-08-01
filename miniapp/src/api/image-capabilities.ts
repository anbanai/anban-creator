import { get } from './request'
import type { ImageCapabilityListResponse } from '@/types'

export const imageCapabilitiesApi = {
  list: () => get<ImageCapabilityListResponse>('/image-capabilities'),
}
