import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { UploadToOSSOptions, UploadToOSSResult } from '@/lib/direct-upload'
import type { InputAttachment } from '@/types/input-attachment'
import { AttachmentRejectionReason } from './attachment-admission'
import { usePromptAttachments } from './usePromptAttachments'

function fileOf(name: string, type = 'image/png', size = 5, lastModified = 1) {
  const file = new File(['x'], name, { type, lastModified })
  Object.defineProperty(file, 'size', { value: size })
  return file
}

function uploadResult(file: File, suffix = file.name): UploadToOSSResult {
  return {
    uploadId: `upload-${suffix}`,
    key: `uploads/pending/user/upload-${suffix}/${file.name}`,
    publicUrl: `https://cdn.example.com/${file.name}?signature=secret`,
    contentType: file.type,
    size: file.size,
  }
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

function idSequence() {
  let id = 0
  return () => `attachment-${++id}`
}

const policy = {
  allowedTypes: ['image', 'audio', 'video', 'document', 'text'] as const,
  maxCount: 8,
}

describe('usePromptAttachments', () => {
  it('hydrates finalized attachments without File or preview URL and serializes key identity only', () => {
    const inherited: InputAttachment = {
      type: 'image',
      upload_id: 'upload-final',
      key: 'tasks/task-1/input/final.png',
      url: 'https://cdn.example.com/signed-final.png',
      file_name: 'final.png',
      content_type: 'image/png',
      size: 12,
      instruction: '保留主体',
      role: 'reference',
    }
    const createObjectURL = vi.fn()
    const { result } = renderHook(() => usePromptAttachments({
      adapter: { mode: 'direct' },
      policy,
      initialAttachments: [inherited],
      createId: idSequence(),
      createObjectURL,
    }))

    expect(result.current.attachments).toEqual([expect.objectContaining({
      id: 'attachment-1',
      status: 'uploaded',
      progress: 100,
      fileName: 'final.png',
      uploadId: 'upload-final',
      key: 'tasks/task-1/input/final.png',
    })])
    expect(result.current.attachments[0]).not.toHaveProperty('file')
    expect(result.current.previewSource('attachment-1')).toBeUndefined()
    expect(createObjectURL).not.toHaveBeenCalled()
    expect(result.current.toInputAttachments()).toEqual([{
      type: 'image',
      upload_id: 'upload-final',
      key: 'tasks/task-1/input/final.png',
      file_name: 'final.png',
      content_type: 'image/png',
      size: 12,
      instruction: '保留主体',
      role: 'reference',
    }])
    expect(JSON.stringify(result.current.toInputAttachments())).not.toContain('cdn.example.com')
  })

  it('round-trips trusted inherited URL, key-only, and inline-text sources', () => {
    const inherited: InputAttachment[] = [
      {
        type: 'image',
        url: '/api/v1/files/tasks/task-1/input/legacy.png',
        key: 'tasks/task-1/input/legacy.png',
        file_name: 'legacy.png',
        content_type: 'image/png',
        size: 20,
        role: 'reference',
        instruction: '保留旧素材',
      },
      {
        type: 'document',
        key: 'tasks/task-1/input/brief.pdf',
        file_name: 'brief.pdf',
        content_type: 'application/pdf',
        size: 30,
        instruction: '阅读简报',
      },
      {
        type: 'text',
        text: '原始内联说明',
        file_name: 'brief.txt',
        content_type: 'text/plain',
        size: 8,
        role: 'brief',
        instruction: '逐字保留',
      },
    ]
    const { result } = renderHook(() => usePromptAttachments({
      adapter: { mode: 'direct' },
      policy,
      initialAttachments: inherited,
      createId: idSequence(),
      createObjectURL: vi.fn(),
    }))

    expect(result.current.toInputAttachments()).toEqual(inherited)
    expect(result.current.attachments).toHaveLength(3)
    for (const attachment of result.current.attachments) {
      expect(attachment).not.toHaveProperty('url')
      expect(attachment).not.toHaveProperty('text')
      expect(attachment).not.toHaveProperty('file')
    }
  })

  it('uploads direct files concurrently while preserving add order and progress', async () => {
    const first = fileOf('first.png')
    const second = fileOf('second.pdf', 'application/pdf')
    const pending = new Map<string, ReturnType<typeof deferred<UploadToOSSResult>>>()
    const upload = vi.fn(({ file }: UploadToOSSOptions) => {
      const operation = deferred<UploadToOSSResult>()
      pending.set(file.name, operation)
      return operation.promise
    })
    const { result } = renderHook(() => usePromptAttachments({
      adapter: { mode: 'direct' },
      policy,
      upload,
      createId: idSequence(),
      createObjectURL: (file) => `blob:${file.name}`,
      revokeObjectURL: vi.fn(),
    }))

    let admission: ReturnType<typeof result.current.addFiles> | undefined
    act(() => {
      admission = result.current.addFiles([first, second])
    })

    expect(admission?.rejected).toEqual([])
    expect(result.current.attachments.map(({ fileName }) => fileName)).toEqual(['first.png', 'second.pdf'])
    expect(result.current.uploading).toBe(true)
    expect(upload).toHaveBeenCalledTimes(2)
    expect(upload).toHaveBeenNthCalledWith(1, expect.objectContaining({
      purpose: 'ai_entry_attachment',
      file: first,
    }))
    expect(result.current.previewSource('attachment-1')).toBe('blob:first.png')

    act(() => {
      upload.mock.calls[0][0].onProgress?.(37)
    })
    expect(result.current.attachments[0].progress).toBe(37)
    expect(result.current.toInputAttachments()).toEqual([])

    await act(async () => {
      pending.get('second.pdf')?.resolve(uploadResult(second))
      await Promise.resolve()
    })
    expect(result.current.attachments.map(({ status }) => status)).toEqual(['uploading', 'uploaded'])
    expect(result.current.toInputAttachments().map(({ file_name }) => file_name)).toEqual(['second.pdf'])

    await act(async () => {
      pending.get('first.png')?.resolve(uploadResult(first))
      await Promise.resolve()
    })
    await waitFor(() => expect(result.current.uploading).toBe(false))

    expect(result.current.attachments.map(({ fileName }) => fileName)).toEqual(['first.png', 'second.pdf'])
    expect(result.current.toInputAttachments()).toEqual([
      expect.objectContaining({
        type: 'image',
        upload_id: 'upload-first.png',
        key: 'uploads/pending/user/upload-first.png/first.png',
        file_name: 'first.png',
      }),
      expect.objectContaining({
        type: 'document',
        upload_id: 'upload-second.pdf',
        key: 'uploads/pending/user/upload-second.pdf/second.pdf',
        file_name: 'second.pdf',
      }),
    ])
    expect(JSON.stringify(result.current.toInputAttachments())).not.toContain('publicUrl')
    expect(JSON.stringify(result.current.toInputAttachments())).not.toContain('cdn.example.com')
  })

  it('marks failures and retries failed attachments only', async () => {
    const file = fileOf('retry.png')
    const upload = vi.fn()
      .mockRejectedValueOnce(new Error('network unavailable'))
      .mockResolvedValueOnce(uploadResult(file, 'retried'))
    const { result } = renderHook(() => usePromptAttachments({
      adapter: { mode: 'direct' },
      policy,
      upload,
      createId: idSequence(),
      createObjectURL: (item) => `blob:${item.name}`,
      revokeObjectURL: vi.fn(),
    }))

    act(() => {
      result.current.addFiles([file])
    })
    await waitFor(() => expect(result.current.hasFailures).toBe(true))
    expect(result.current.attachments[0]).toEqual(expect.objectContaining({
      status: 'failed',
      error: 'network unavailable',
    }))
    expect(result.current.toInputAttachments()).toEqual([])

    act(() => {
      result.current.retry('missing')
      result.current.retry('attachment-1')
    })
    await waitFor(() => expect(result.current.attachments[0].status).toBe('uploaded'))
    expect(upload).toHaveBeenCalledTimes(2)
    expect(result.current.hasFailures).toBe(false)

    act(() => result.current.retry('attachment-1'))
    expect(upload).toHaveBeenCalledTimes(2)
  })

  it('keeps local files queued for multipart submission in stable accepted order', () => {
    const upload = vi.fn()
    const first = fileOf('first.txt', 'text/plain')
    const duplicate = fileOf('first.txt', 'text/plain')
    const second = fileOf('second.png')
    const { result } = renderHook(() => usePromptAttachments({
      adapter: { mode: 'local' },
      policy,
      upload,
      createId: idSequence(),
      createObjectURL: (file) => `blob:${file.name}`,
      revokeObjectURL: vi.fn(),
    }))

    let admission: ReturnType<typeof result.current.addFiles> | undefined
    act(() => {
      admission = result.current.addFiles([first, duplicate, second])
    })

    expect(admission?.rejected).toEqual([{
      file: duplicate,
      reason: AttachmentRejectionReason.Duplicate,
    }])
    expect(result.current.attachments.map(({ status }) => status)).toEqual(['queued', 'queued'])
    expect(result.current.localFiles()).toEqual([first, second])
    expect(result.current.toInputAttachments()).toEqual([])
    expect(result.current.uploading).toBe(false)
    expect(upload).not.toHaveBeenCalled()
  })

  it('uploads designer references with their dedicated purpose and exposes uploaded identities', async () => {
    const file = fileOf('design.png')
    const upload = vi.fn().mockResolvedValue(uploadResult(file))
    const { result } = renderHook(() => usePromptAttachments({
      adapter: { mode: 'designer' },
      policy,
      upload,
      createId: idSequence(),
      createObjectURL: (item) => `blob:${item.name}`,
      revokeObjectURL: vi.fn(),
    }))

    act(() => {
      result.current.addFiles([file])
    })
    await waitFor(() => expect(result.current.attachments[0].status).toBe('uploaded'))

    expect(upload).toHaveBeenCalledWith(expect.objectContaining({
      purpose: 'designer_reference',
      file,
    }))
    expect(result.current.attachments[0]).toEqual(expect.objectContaining({
      uploadId: 'upload-design.png',
      key: 'uploads/pending/user/upload-design.png/design.png',
    }))
    expect(result.current.toInputAttachments()).toEqual([expect.objectContaining({
      upload_id: 'upload-design.png',
      key: 'uploads/pending/user/upload-design.png/design.png',
    })])
  })

  it('revokes each transient URL once and ignores stale async completion', async () => {
    const operations: ReturnType<typeof deferred<UploadToOSSResult>>[] = []
    const signals: Array<AbortSignal | undefined> = []
    const upload = vi.fn(({ file, signal }: UploadToOSSOptions) => {
      const operation = deferred<UploadToOSSResult>()
      operations.push(operation)
      signals.push(signal)
      signal?.addEventListener('abort', () => {
        operation.reject(new DOMException('Upload aborted', 'AbortError'))
      }, { once: true })
      return operation.promise.then(() => uploadResult(file))
    })
    const revokeObjectURL = vi.fn()
    const { result, unmount } = renderHook(() => usePromptAttachments({
      adapter: { mode: 'direct' },
      policy,
      upload,
      createId: idSequence(),
      createObjectURL: (file) => `blob:${file.name}`,
      revokeObjectURL,
    }))

    act(() => result.current.addFiles([fileOf('removed.png')]))
    act(() => result.current.remove('attachment-1'))
    expect(result.current.attachments).toEqual([])
    expect(signals[0]?.aborted).toBe(true)
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:removed.png')

    await act(async () => {
      operations[0].resolve(uploadResult(fileOf('removed.png')))
      await Promise.resolve()
    })
    expect(result.current.attachments).toEqual([])

    act(() => result.current.addFiles([fileOf('cleared.png')]))
    act(() => result.current.clear())
    expect(result.current.attachments).toEqual([])
    expect(signals[1]?.aborted).toBe(true)

    act(() => result.current.addFiles([fileOf('unmounted.png')]))
    unmount()
    expect(signals[2]?.aborted).toBe(true)
    await act(async () => {
      operations[1].resolve(uploadResult(fileOf('cleared.png')))
      operations[2].resolve(uploadResult(fileOf('unmounted.png')))
      await Promise.resolve()
    })

    expect(revokeObjectURL.mock.calls.map(([url]) => url)).toEqual([
      'blob:removed.png',
      'blob:cleared.png',
      'blob:unmounted.png',
    ])
  })
})
