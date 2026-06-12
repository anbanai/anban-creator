import { get, post } from './request'
import type { DesignerProvider, GenerateRequest, HistoryResponse, ImageGeneration } from '@/types'
import { TOKEN_KEY } from '@/utils/constants'

export const designerApi = {
  getProviders: () =>
    get<DesignerProvider[]>('/designer/providers'),

  generate: (req: GenerateRequest) =>
    post<{ generation_id: string; status: string }>('/designer/generate', req),

  uploadReference: (filePath: string, name = 'file') =>
    new Promise<{ file_id: string; filename: string; size: number }>((resolve, reject) => {
      const token = uni.getStorageSync(TOKEN_KEY)
      uni.uploadFile({
        url: '/api/v1/designer/upload-reference',
        filePath,
        name,
        header: token ? { Authorization: `Bearer ${token}` } : undefined,
        success(res) {
          try {
            const body = JSON.parse(res.data)
            if (body.code === 0) {
              resolve(body.data)
            } else {
              reject(new Error(body.msg || '上传失败'))
            }
          } catch {
            reject(new Error('上传响应解析失败'))
          }
        },
        fail(err) {
          reject(new Error(err.errMsg || '上传失败'))
        },
      })
    }),

  getHistory: (params: { channel_id?: string; page?: number; page_size?: number } = {}) =>
    get<HistoryResponse>('/designer/history', params as Record<string, any>),

  getGeneration: (id: string) =>
    get<ImageGeneration>(`/designer/generations/${id}`),
}
