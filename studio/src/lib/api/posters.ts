import { http, unwrap } from '@/lib/http-client'
import type { PosterTask, CreatePosterRequest } from '@/types'

export const postersApi = {
  create: (data: CreatePosterRequest) =>
    unwrap<PosterTask>(http.post('/posters', data)),

  get: (id: string) =>
    unwrap<PosterTask>(http.get(`/posters/${id}`)),

  list: (params?: { offset?: number; limit?: number }) =>
    unwrap<{ items: PosterTask[]; total: number }>(http.get('/posters', { params })),
}
