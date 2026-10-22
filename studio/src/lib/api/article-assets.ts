import { http } from '@/lib/http-client'

export const articleAssetsApi = {
  remove: (id: string) => http.delete(`/article-assets/${encodeURIComponent(id)}`),
}
