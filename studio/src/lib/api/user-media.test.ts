import { afterEach, describe, expect, it, vi } from 'vitest'

import { http } from '@/lib/http-client'
import { userMediaApi } from './user-media'

describe('userMediaApi', () => {
  afterEach(() => vi.restoreAllMocks())

  it('lists ready media with server-side filters and creates a library record from an upload session', async () => {
    const get = vi.spyOn(http, 'get').mockResolvedValue({ data: { data: { items: [], total: 0, offset: 0, limit: 30 } } } as never)
    const post = vi.spyOn(http, 'post').mockResolvedValue({ data: { data: { id: 'media-1', status: 'queued' } } } as never)

    await expect(userMediaApi.list({ q: '包装', media_type: 'image', project_id: 'project-1', status: 'available', limit: 30 })).resolves.toEqual({ items: [], total: 0, offset: 0, limit: 30 })
    await expect(userMediaApi.create({ upload_session_id: 'upload-session-1', project_id: 'project-1' })).resolves.toEqual({ id: 'media-1', status: 'queued' })

    expect(get).toHaveBeenCalledWith('/media-assets', { params: { q: '包装', media_type: 'image', project_id: 'project-1', status: 'available', limit: 30 } })
    expect(post).toHaveBeenCalledWith('/media-assets', { upload_session_id: 'upload-session-1', project_id: 'project-1' })
  })

  it('keeps reanalysis and removal as explicit asset actions', async () => {
    const post = vi.spyOn(http, 'post').mockResolvedValue({ data: { data: { id: 'media-1', status: 'queued' } } } as never)
    const del = vi.spyOn(http, 'delete').mockResolvedValue({ data: { data: { removed: true } } } as never)

    await userMediaApi.reanalyze('media-1')
    await userMediaApi.remove('media-1')

    expect(post).toHaveBeenCalledWith('/media-assets/media-1/reanalyze')
    expect(del).toHaveBeenCalledWith('/media-assets/media-1')
  })
})
