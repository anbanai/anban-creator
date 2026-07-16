import { beforeEach, describe, expect, it, vi } from 'vitest'
import http from '@/lib/http-client'
import { directUploadResultToInputAttachment, uploadToOSS } from '@/lib/direct-upload'

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

function fileOf(size: number, type = 'image/png', name = 'asset.png') {
  const file = new File(['x'], name, { type })
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
          headers: { 'Content-Type': 'image/png' },
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

  it('lets the server infer content type when the browser provides none', async () => {
    vi.mocked(http.post).mockResolvedValueOnce({
      data: {
        data: {
          upload_id: 'up-audio',
          key: 'uploads/pending/user/up-audio/sound.m4a',
          public_url: 'https://cdn.example.com/uploads/pending/user/up-audio/sound.m4a',
          region: 'oss-cn-hangzhou',
          bucket: 'bucket',
          endpoint: 'oss-cn-hangzhou.aliyuncs.com',
          sts_access_key_id: 'sts-ak',
          sts_access_key_secret: 'sts-secret',
          sts_security_token: 'sts-token',
          expires_at: '2026-07-03T10:00:00Z',
          max_size: 50 * 1024 * 1024,
          headers: { 'Content-Type': 'audio/mp4' },
        },
      },
    })

    const result = await uploadToOSS({
      purpose: 'video_reference',
      file: fileOf(1024, '', 'sound.m4a'),
    })

    expect(http.post).toHaveBeenCalledWith('/uploads/prepare', {
      purpose: 'video_reference',
      filename: 'sound.m4a',
      content_type: '',
      size: 1024,
    })
    expect(putMock).toHaveBeenCalledWith('uploads/pending/user/up-audio/sound.m4a', expect.any(File), expect.objectContaining({
      headers: expect.objectContaining({ 'Content-Type': 'audio/mp4' }),
    }))
    expect(result.contentType).toBe('audio/mp4')
  })

  it('fails fast when STS assume-role credentials cannot be issued', async () => {
    vi.mocked(http.post).mockRejectedValueOnce({
      response: {
        status: 503,
        data: {
          msg: 'issue upload credential: refresh session token failed: {"Code":"NoPermission","AuthAction":"sts:AssumeRole"}',
        },
      },
    })

    await expect(uploadToOSS({
      purpose: 'project_reference',
      file: fileOf(1024),
    })).rejects.toThrow('当前环境 OSS 直传凭证不可用，请联系管理员检查 RAM/STS 权限。')

    expect(http.post).toHaveBeenCalledWith('/uploads/prepare', {
      purpose: 'project_reference',
      filename: 'asset.png',
      content_type: 'image/png',
      size: 1024,
    })
    expect(http.post).toHaveBeenCalledTimes(1)
    expect(putMock).not.toHaveBeenCalled()
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

describe('directUploadResultToInputAttachment', () => {
  it('keeps pending upload identity without serializing the public URL', () => {
    const file = fileOf(2048, 'image/png', '产品图.png')

    const attachment = directUploadResultToInputAttachment({
      uploadId: 'up-composer',
      key: 'uploads/pending/user/up-composer/product.png',
      publicUrl: 'https://cdn.example.com/signed-or-public.png',
      contentType: 'image/png',
      size: 2048,
    }, file, 'image', '保留包装')

    expect(attachment).toEqual({
      type: 'image',
      upload_id: 'up-composer',
      key: 'uploads/pending/user/up-composer/product.png',
      file_name: '产品图.png',
      content_type: 'image/png',
      size: 2048,
      instruction: '保留包装',
    })
    expect(attachment).not.toHaveProperty('url')
    expect(JSON.stringify(attachment)).not.toContain('cdn.example.com')
  })
})
