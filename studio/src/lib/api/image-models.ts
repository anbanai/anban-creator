import { http, unwrap } from '@/lib/http-client'
import type { ImageModelListResponse } from '@/types'

export const imageModelsApi = {
  list: () =>
    unwrap<ImageModelListResponse>(http.get('/image-models')),
}
