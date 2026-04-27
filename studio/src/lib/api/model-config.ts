import { http } from '@/lib/http-client'
import type { ApiResponse } from '@/types'

export interface TextConfigDTO {
  base_url?: string
  api_key?: string
  model?: string
  proxy?: string
}

export interface ImageConfigDTO {
  provider?: string
  base_url?: string
  api_key?: string
  model?: string
  proxy?: string
}

export interface ModelConfigResponse {
  text?: TextConfigDTO | null
  image?: ImageConfigDTO | null
}

export interface UpdateModelConfigRequest {
  text?: TextConfigDTO | null
  image?: ImageConfigDTO | null
}

export const modelConfigApi = {
  get: () => http.get<ApiResponse<ModelConfigResponse>>('/model-config').then((r) => r.data.data),

  update: (data: UpdateModelConfigRequest) =>
    http.put<ApiResponse>('/model-config', data).then((r) => r.data),

  clear: () => http.delete<ApiResponse>('/model-config').then((r) => r.data),
}
