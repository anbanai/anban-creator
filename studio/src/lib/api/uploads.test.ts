import { afterEach, describe, expect, it, vi } from 'vitest'
import { http } from '@/lib/http-client'
import { uploadsApi } from './uploads'

describe('uploadsApi', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('posts stable attachment identity to resolve a fresh download URL', async () => {
    const payload = {
      upload_id: 'upload-1',
      key: 'uploads/finalized/user-1/upload-1/input.png',
      owner_type: 'task' as const,
      owner_id: 'task-1',
    }
    const post = vi.spyOn(http, 'post').mockResolvedValue({
      data: {
        code: 0,
        msg: 'success',
        data: { url: 'https://signed.example.com/input.png', expires_at: '2026-07-15T12:00:00Z' },
      },
    })

    const result = await uploadsApi.resolveDownloadUrl(payload)

    expect(post).toHaveBeenCalledWith('/uploads/resolve-download-url', payload)
    expect(result).toEqual({
      url: 'https://signed.example.com/input.png',
      expires_at: '2026-07-15T12:00:00Z',
    })
  })
})
