import { get } from './request'
import type { ImageModelListResponse } from '@/types'

export const imageModelsApi = {
  /** List the image models available to the current user (tier-gated). */
  list: () => get<ImageModelListResponse>('/image-models'),
}
