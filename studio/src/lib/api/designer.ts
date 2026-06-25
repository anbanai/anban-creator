import { http, unwrap } from '@/lib/http-client'
import type { DesignerProvider, GenerateRequest, HistoryResponse, ImageGeneration } from '@/types/designer'

export const designerApi = {
  getProviders: () =>
    unwrap<DesignerProvider[]>(http.get('/designer/providers')),

  generate: (req: GenerateRequest) =>
    unwrap<{ generation_id: string; status: string }>(http.post('/designer/generate', req)),

  uploadReference: (file: File) => {
    const form = new FormData()
    form.append('file', file)
    return unwrap<{ file_id: string; filename: string; size: number }>(
      http.post('/designer/upload-reference', form, {
        headers: { 'Content-Type': 'multipart/form-data' },
      }),
    )
  },

  uploadReferenceFromUrl: (url: string) =>
    unwrap<{ file_id: string; filename: string; size: number }>(
      http.post('/designer/upload-reference-from-url', { url }),
    ),

  getHistory: (params: { project_id?: string; page?: number; page_size?: number } = {}) =>
    unwrap<HistoryResponse>(http.get('/designer/history', { params })),

  getGeneration: (id: string) =>
    unwrap<ImageGeneration>(http.get(`/designer/generations/${id}`)),
}
