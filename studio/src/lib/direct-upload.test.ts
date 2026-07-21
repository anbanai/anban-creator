import { beforeEach, describe, expect, it, vi } from 'vitest'
import http from '@/lib/http-client'
import { directUploadResultToInputAttachment, uploadToOSS } from '@/lib/direct-upload'

const putMock = vi.fn()
const multipartUploadMock = vi.fn()
const cancelMock = vi.fn()
const abortMultipartUploadMock = vi.fn()

let xhrAutoComplete = true
const xhrInstances: FakeXMLHttpRequest[] = []

class FakeXMLHttpRequest {
  status = 0
  responseText = ''
  onload: (() => void) | null = null
  onerror: (() => void) | null = null
  onabort: (() => void) | null = null
  upload = { onprogress: null as ((event: ProgressEvent) => void) | null }
  open = vi.fn()
  setRequestHeader = vi.fn()
  send = vi.fn(() => {
    if (xhrAutoComplete) {
      queueMicrotask(() => this.finish(200))
    }
  })
  abort = vi.fn(() => this.onabort?.())

  constructor() {
    xhrInstances.push(this)
  }

  finish(status: number, responseText = '') {
    this.status = status
    this.responseText = responseText
    this.onload?.()
  }
}

vi.mock('ali-oss', () => ({
  default: vi.fn(function OSSClient() {
    return {
    put: putMock,
    multipartUpload: multipartUploadMock,
    cancel: cancelMock,
    abortMultipartUpload: abortMultipartUploadMock,
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

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

describe('uploadToOSS', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    xhrInstances.length = 0
    xhrAutoComplete = true
    vi.stubGlobal('XMLHttpRequest', FakeXMLHttpRequest)
    vi.mocked(http.post).mockResolvedValue({
      data: {
        data: {
          upload_session_id: 'session-1',
          upload_id: 'up-1',
          key: 'uploads/pending/user/up-1/asset.png',
          preview_url: 'https://signed.example.com/uploads/pending/user/up-1/asset.png',
          public_url: 'https://cdn.example.com/uploads/pending/user/up-1/asset.png',
          upload_url: 'https://oss-upload.example.com/uploads/pending/user/up-1/asset.png?signature=put',
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

  it('uploads small files through the signed PUT URL', async () => {
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
    expect(xhrInstances).toHaveLength(1)
    expect(xhrInstances[0].open).toHaveBeenCalledWith(
      'PUT',
      'https://oss-upload.example.com/uploads/pending/user/up-1/asset.png?signature=put',
      true,
    )
    expect(xhrInstances[0].setRequestHeader).toHaveBeenCalledWith('Content-Type', 'image/png')
    expect(xhrInstances[0].send).toHaveBeenCalledWith(expect.any(File))
    expect(putMock).not.toHaveBeenCalled()
    expect(multipartUploadMock).not.toHaveBeenCalled()
    expect(progress[progress.length - 1]).toBe(100)
    expect(result.uploadSessionId).toBe('session-1')
    expect(result.previewUrl).toBe('https://signed.example.com/uploads/pending/user/up-1/asset.png')
    expect(result.publicUrl).toBe('https://cdn.example.com/uploads/pending/user/up-1/asset.png')
  })

  it('uses multipart upload for large video references and reports progress', async () => {
    const progress: number[] = []
    await uploadToOSS({
      purpose: 'ai_entry_attachment',
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
          upload_url: 'https://oss-upload.example.com/uploads/pending/user/up-audio/sound.m4a?signature=put',
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
      purpose: 'ai_entry_attachment',
      file: fileOf(1024, '', 'sound.m4a'),
    })

    expect(http.post).toHaveBeenCalledWith('/uploads/prepare', {
      purpose: 'ai_entry_attachment',
      filename: 'sound.m4a',
      content_type: '',
      size: 1024,
    })
    expect(xhrInstances[0].setRequestHeader).toHaveBeenCalledWith('Content-Type', 'audio/mp4')
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
    multipartUploadMock
      .mockRejectedValueOnce(Object.assign(new Error('Request has expired'), { code: 'AccessDenied' }))
      .mockRejectedValueOnce(Object.assign(new Error('Request has expired'), { code: 'AccessDenied' }))

    await expect(uploadToOSS({ purpose: 'project_reference', file: fileOf(12 * 1024 * 1024) }))
      .rejects.toThrow('上传凭证已过期，请重试上传')
    expect(http.post).toHaveBeenCalledTimes(2)
    expect(multipartUploadMock).toHaveBeenCalledTimes(2)
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
    multipartUploadMock
      .mockRejectedValueOnce(Object.assign(new Error('Request has expired'), { code: 'AccessDenied' }))
      .mockResolvedValueOnce({})

    const result = await uploadToOSS({ purpose: 'project_reference', file: fileOf(12 * 1024 * 1024) })

    expect(http.post).toHaveBeenCalledTimes(2)
    expect(multipartUploadMock).toHaveBeenNthCalledWith(1, 'uploads/pending/user/up-1/asset.png', expect.any(File), expect.any(Object))
    expect(multipartUploadMock).toHaveBeenNthCalledWith(2, 'uploads/pending/user/up-2/asset.png', expect.any(File), expect.any(Object))
    expect(result.uploadId).toBe('up-2')
    expect(result.publicUrl).toBe('https://cdn.example.com/uploads/pending/user/up-2/asset.png')
  })

  it('passes AbortSignal to prepare and aborts an in-flight signed PUT without retrying', async () => {
    xhrAutoComplete = false
    const controller = new AbortController()
    const promise = uploadToOSS({
      purpose: 'ai_entry_attachment',
      file: fileOf(1024),
      signal: controller.signal,
    })

    await vi.waitFor(() => expect(xhrInstances).toHaveLength(1))
    expect(http.post).toHaveBeenCalledWith('/uploads/prepare', expect.any(Object), {
      signal: controller.signal,
    })

    controller.abort()

    await expect(promise).rejects.toMatchObject({ name: 'AbortError' })
    expect(xhrInstances[0].abort).toHaveBeenCalledTimes(1)
    expect(http.post).toHaveBeenCalledTimes(1)
  })

  it('short-circuits an already aborted upload before preparing credentials', async () => {
    const controller = new AbortController()
    controller.abort()

    await expect(uploadToOSS({
      purpose: 'ai_entry_attachment',
      file: fileOf(1024),
      signal: controller.signal,
    })).rejects.toMatchObject({ name: 'AbortError' })

    expect(http.post).not.toHaveBeenCalled()
  })

  it('cancels the remote multipart upload by checkpoint when abort follows progress', async () => {
    const controller = new AbortController()
    const cleanup = deferred<void>()
    let rejectUpload!: (reason?: unknown) => void
    multipartUploadMock.mockImplementationOnce(() => new Promise((_, reject) => {
      rejectUpload = reject
    }))
    cancelMock.mockImplementation(() => rejectUpload(new Error('cancelled by SDK')))
    abortMultipartUploadMock.mockReturnValueOnce(cleanup.promise)
    const promise = uploadToOSS({
      purpose: 'ai_entry_attachment',
      file: fileOf(12 * 1024 * 1024, 'video/mp4'),
      signal: controller.signal,
    })
    await vi.waitFor(() => expect(multipartUploadMock).toHaveBeenCalledTimes(1))
    const progress = multipartUploadMock.mock.calls[0][2].progress
    const checkpoint = { name: 'ignored-client-name', uploadId: 'multipart-upload-1' }
    progress(0.4, checkpoint)

    controller.abort()
    progress(0.5, checkpoint)

    let settled = false
    void promise.then(() => { settled = true }, () => { settled = true })
    await Promise.resolve()
    expect(settled).toBe(false)
    expect(cancelMock).toHaveBeenCalledOnce()
    expect(cancelMock.mock.calls[0]).toEqual([])
    expect(abortMultipartUploadMock).toHaveBeenCalledOnce()
    expect(abortMultipartUploadMock).toHaveBeenCalledWith(
      'uploads/pending/user/up-1/asset.png',
      'multipart-upload-1',
    )

    cleanup.resolve()
    await expect(promise).rejects.toMatchObject({ name: 'AbortError' })
    expect(http.post).toHaveBeenCalledTimes(1)
  })

  it('cancels the queue before a checkpoint and aborts the remote upload when it arrives', async () => {
    const controller = new AbortController()
    const cleanup = deferred<void>()
    let rejectUpload!: (reason?: unknown) => void
    multipartUploadMock.mockImplementationOnce(() => new Promise((_, reject) => {
      rejectUpload = reject
    }))
    cancelMock.mockImplementation(() => rejectUpload(new Error('cancelled by SDK')))
    abortMultipartUploadMock.mockReturnValueOnce(cleanup.promise)
    const promise = uploadToOSS({
      purpose: 'ai_entry_attachment',
      file: fileOf(12 * 1024 * 1024, 'video/mp4'),
      signal: controller.signal,
    })
    await vi.waitFor(() => expect(multipartUploadMock).toHaveBeenCalledTimes(1))
    const progress = multipartUploadMock.mock.calls[0][2].progress

    controller.abort()
    expect(cancelMock).toHaveBeenCalledOnce()
    expect(cancelMock.mock.calls[0]).toEqual([])
    expect(abortMultipartUploadMock).not.toHaveBeenCalled()

    const checkpoint = { uploadId: 'multipart-upload-late' }
    progress(0.1, checkpoint)
    progress(0.2, checkpoint)

    let settled = false
    void promise.then(() => { settled = true }, () => { settled = true })
    await Promise.resolve()
    expect(settled).toBe(false)
    expect(cancelMock).toHaveBeenCalledOnce()
    expect(abortMultipartUploadMock).toHaveBeenCalledOnce()
    expect(abortMultipartUploadMock).toHaveBeenCalledWith(
      'uploads/pending/user/up-1/asset.png',
      'multipart-upload-late',
    )

    cleanup.reject(new Error('remote cleanup failed'))
    await expect(promise).rejects.toMatchObject({ name: 'AbortError' })
    expect(http.post).toHaveBeenCalledTimes(1)
  })

  it('rejects promptly when multipart cancellation settles without a checkpoint', async () => {
    const controller = new AbortController()
    let rejectUpload!: (reason?: unknown) => void
    multipartUploadMock.mockImplementationOnce(() => new Promise((_, reject) => {
      rejectUpload = reject
    }))
    cancelMock.mockImplementation(() => rejectUpload(new Error('cancelled by SDK')))
    const promise = uploadToOSS({
      purpose: 'ai_entry_attachment',
      file: fileOf(12 * 1024 * 1024, 'video/mp4'),
      signal: controller.signal,
    })
    await vi.waitFor(() => expect(multipartUploadMock).toHaveBeenCalledTimes(1))

    controller.abort()

    const outcome = await Promise.race([
      promise.then(
        () => ({ state: 'resolved' as const }),
        (error: unknown) => ({ state: 'rejected' as const, error }),
      ),
      new Promise<{ state: 'timeout' }>((resolve) => {
        setTimeout(() => resolve({ state: 'timeout' }), 250)
      }),
    ])
    expect(outcome).toMatchObject({
      state: 'rejected',
      error: { name: 'AbortError' },
    })
    expect(cancelMock).toHaveBeenCalledOnce()
    expect(cancelMock.mock.calls[0]).toEqual([])
    expect(abortMultipartUploadMock).not.toHaveBeenCalled()
    expect(http.post).toHaveBeenCalledTimes(1)
  })
})

describe('directUploadResultToInputAttachment', () => {
  it('keeps pending upload identity without serializing the public URL', () => {
    const file = fileOf(2048, 'image/png', '产品图.png')

    const attachment = directUploadResultToInputAttachment({
      uploadSessionId: 'session-composer',
      uploadId: 'up-composer',
      key: 'uploads/pending/user/up-composer/product.png',
      previewUrl: 'https://signed.example.com/product.png',
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
