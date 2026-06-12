import { get, put, del } from './request'

export interface TextConfigDTO {
  endpoint?: string
  api_key?: string
  model?: string
  proxy?: string
}

export interface ImageConfigDTO {
  provider?: string
  endpoint?: string
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
  get: () =>
    get<ModelConfigResponse>('/model-config'),

  update: (data: UpdateModelConfigRequest) =>
    put<void>('/model-config', data),

  clear: () =>
    del<void>('/model-config'),
}
