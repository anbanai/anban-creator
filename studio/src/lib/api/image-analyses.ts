import { http, unwrap } from '@/lib/http-client'
import type { ImageAnalysis } from '@/types'

export const imageAnalysesApi = {
  retry: (id: string) => unwrap<ImageAnalysis>(http.post(`/image-analyses/${id}/retry`)),
  cancel: (id: string) => unwrap<{ cancelled: boolean }>(http.post(`/image-analyses/${id}/cancel`)),
}
