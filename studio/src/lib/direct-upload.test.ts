import { beforeEach, describe, expect, it, vi } from 'vitest'
import http from '@/lib/http-client'
import { uploadToOSS } from '@/lib/direct-upload'

const putMock = vi.fn()
const multipartUploadMock = vi.fn()

vi.mock('ali-oss', () => ({
  default: vi.fn(function OSSClient() {
    return {
    put: putMock,
    multipartUpload: multipartUploadMock,
    }
  }),
}))

vi.mock('@/lib/http-client', () => ({
  default: {
    post: vi.fn(),
  },
}))

function fileOf(size: number, type = 'image/png') {
  const file = new File(['x'], 'asset.png', { type })
  Object.defineProperty(file, 'size', { value: size })
  return file
}

describe('uploadToOSS', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(http.post).mockResolvedValue({
      data: {
        data: {
          upload_id: 'up-1',
          key: 'uploads/pending/user/up-1/asset.png',
          public_url: 'https://cdn.example.com/uploads/pending/user/up-1/asset.png',
          region: 'oss-cn-hangzhou',
          bucket: 'bucket',
          endpoint: 'oss-cn-hangzhou.aliyuncs.com',
          sts_access_key_id: 'sts-ak',
          sts_access_key_secret: 'sts-secret',
          sts_security_token: 'sts-token',
          expires_at: '2026-07-03T10:00:00Z',
          max_size: 50 * 1024 * 1024,
        },
      },
    })
    putMock.mockResolvedValue({})
    multipartUploadMock.mockResolvedValue({})
  })

  it('prepares credentials and uploads small files with ali-oss put', async () => {
    const progress: number[] = []
    const result = await uploadToOSS({
      purpose: 'project_reference',
      file: fileOf(1024),
      onProgress: (percent) => progress.push(percent),
    })

    expect(http.post).toHaveBeenCalledWith('/uploads/prepare', {
      purpose: 'project_reference',
      filename: 'asset.png',
      content_type: 'image/png',
      size: 1024,
    })
    expect(putMock).toHaveBeenCalledWith('uploads/pending/user/up-1/asset.png', expect.any(File), expect.objectContaining({
      headers: expect.objectContaining({ 'Content-Type': 'image/png' }),
    }))
    expect(multipartUploadMock).not.toHaveBeenCalled()
    expect(progress[progress.length - 1]).toBe(100)
    expect(result.publicUrl).toBe('https://cdn.example.com/uploads/pending/user/up-1/asset.png')
  })

  it('uses multipart upload for large video references and reports progress', async () => {
    const progress: number[] = []
    await uploadToOSS({
      purpose: 'video_reference',
      file: fileOf(12 * 1024 * 1024, 'video/mp4'),
      onProgress: (percent) => progress.push(percent),
    })
    const options = multipartUploadMock.mock.calls[0][2]
    options.progress(0.42)

    expect(putMock).not.toHaveBeenCalled()
    expect(multipartUploadMock).toHaveBeenCalledWith('uploads/pending/user/up-1/asset.png', expect.any(File), expect.any(Object))
    expect(progress).toContain(42)
  })

  it('prepares AI entry attachments with the unified upload purpose', async () => {
    await uploadToOSS({
      purpose: 'ai_entry_attachment',
      file: fileOf(1024, 'application/pdf'),
    })

    expect(http.post).toHaveBeenCalledWith('/uploads/prepare', {
      purpose: 'ai_entry_attachment',
      filename: 'asset.png',
      content_type: 'application/pdf',
      size: 1024,
    })
  })

  it('falls back to the legacy upload endpoint when STS assume-role credentials cannot be issued', async () => {
    vi.mocked(http.post)
      .mockRejectedValueOnce({
        response: {
          status: 503,
          data: {
            msg: 'issue upload credential: refresh session token failed: {"Code":"NoPermission","AuthAction":"sts:AssumeRole"}',
          },
        },
      })
      .mockResolvedValueOnce({
        data: {
          data: {
            url: 'https://cdn.example.com/uploads/projects/user-1/ref.png',
            key: 'uploads/projects/user-1/ref.png',
            size: 1024,
            type: 'image/png',
          },
        },
      })

    const result = await uploadToOSS({
      purpose: 'project_reference',
      file: fileOf(1024),
    })
    const legacyForm = vi.mocked(http.post).mock.calls[1][1] as FormData

    expect(http.post).toHaveBeenNthCalledWith(1, '/uploads/prepare', {
      purpose: 'project_reference',
      filename: 'asset.png',
      content_type: 'image/png',
      size: 1024,
    })
    expect(http.post).toHaveBeenNthCalledWith(2, '/files/upload', expect.any(FormData), expect.any(Object))
    expect(legacyForm.get('purpose')).toBe('project')
    expect(putMock).not.toHaveBeenCalled()
    expect(result).toMatchObject({
      uploadId: 'uploads/projects/user-1/ref.png',
      key: 'uploads/projects/user-1/ref.png',
      publicUrl: 'https://cdn.example.com/uploads/projects/user-1/ref.png',
      contentType: 'image/png',
      size: 1024,
    })
  })

  it('maps expired credential errors to a Chinese retry hint', async () => {
    putMock
      .mockRejectedValueOnce(Object.assign(new Error('Request has expired'), { code: 'AccessDenied' }))
      .mockRejectedValueOnce(Object.assign(new Error('Request has expired'), { code: 'AccessDenied' }))

    await expect(uploadToOSS({ purpose: 'project_reference', file: fileOf(1024) }))
      .rejects.toThrow('上传凭证已过期，请重试上传')
    expect(http.post).toHaveBeenCalledTimes(2)
    expect(putMock).toHaveBeenCalledTimes(2)
  })

  it('re-prepares credentials once when the first OSS upload credential has expired', async () => {
    vi.mocked(http.post)
      .mockResolvedValueOnce({
        data: {
          data: {
            upload_id: 'up-1',
            key: 'uploads/pending/user/up-1/asset.png',
            public_url: 'https://cdn.example.com/uploads/pending/user/up-1/asset.png',
            region: 'oss-cn-hangzhou',
            bucket: 'bucket',
            endpoint: 'oss-cn-hangzhou.aliyuncs.com',
            sts_access_key_id: 'expired-ak',
            sts_access_key_secret: 'expired-secret',
            sts_security_token: 'expired-token',
            expires_at: '2026-07-03T10:00:00Z',
            max_size: 50 * 1024 * 1024,
          },
        },
      })
      .mockResolvedValueOnce({
        data: {
          data: {
            upload_id: 'up-2',
            key: 'uploads/pending/user/up-2/asset.png',
            public_url: 'https://cdn.example.com/uploads/pending/user/up-2/asset.png',
            region: 'oss-cn-hangzhou',
            bucket: 'bucket',
            endpoint: 'oss-cn-hangzhou.aliyuncs.com',
            sts_access_key_id: 'fresh-ak',
            sts_access_key_secret: 'fresh-secret',
            sts_security_token: 'fresh-token',
            expires_at: '2026-07-03T10:00:00Z',
            max_size: 50 * 1024 * 1024,
          },
        },
      })
    putMock
      .mockRejectedValueOnce(Object.assign(new Error('Request has expired'), { code: 'AccessDenied' }))
      .mockResolvedValueOnce({})

    const result = await uploadToOSS({ purpose: 'project_reference', file: fileOf(1024) })

    expect(http.post).toHaveBeenCalledTimes(2)
    expect(putMock).toHaveBeenNthCalledWith(1, 'uploads/pending/user/up-1/asset.png', expect.any(File), expect.any(Object))
    expect(putMock).toHaveBeenNthCalledWith(2, 'uploads/pending/user/up-2/asset.png', expect.any(File), expect.any(Object))
    expect(result.uploadId).toBe('up-2')
    expect(result.publicUrl).toBe('https://cdn.example.com/uploads/pending/user/up-2/asset.png')
  })
})
