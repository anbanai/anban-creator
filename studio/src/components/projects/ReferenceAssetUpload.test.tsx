import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { uploadToOSS } from '@/lib/direct-upload'
import { ReferenceAssetUpload } from './ReferenceAssetUpload'

vi.mock('@/lib/direct-upload', () => ({
  uploadToOSS: vi.fn(),
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

describe('ReferenceAssetUpload', () => {
  const createObjectURL = vi.fn(() => 'blob:reference-preview')
  const revokeObjectURL = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
    class TestURL extends URL {}
    Object.assign(TestURL, { createObjectURL, revokeObjectURL })
    vi.stubGlobal('URL', TestURL)
  })

  afterEach(() => vi.unstubAllGlobals())

  it('keeps the blob preview local and reports only the upload session identity', async () => {
    const upload = deferred<Awaited<ReturnType<typeof uploadToOSS>>>()
    vi.mocked(uploadToOSS).mockReturnValueOnce(upload.promise)
    const onChange = vi.fn()
    const onUploadingChange = vi.fn()
    const file = new File(['reference'], 'reference.png', { type: 'image/png' })

    render(
      <ReferenceAssetUpload
        value={null}
        purpose="project_reference"
        onChange={onChange}
        onUploadingChange={onUploadingChange}
      />,
    )

    fireEvent.change(screen.getByLabelText('参考图文件'), { target: { files: [file] } })

    expect(createObjectURL).toHaveBeenCalledWith(file)
    expect(uploadToOSS).toHaveBeenCalledWith(expect.objectContaining({
      purpose: 'project_reference',
      file,
      signal: expect.any(AbortSignal),
    }))
    expect(screen.getByAltText('参考图')).toHaveAttribute('src', 'blob:reference-preview')
    expect(onUploadingChange).toHaveBeenCalledWith(true)

    upload.resolve({
      uploadId: 'transport-1',
      uploadSessionId: 'session-1',
      key: 'uploads/pending/user/session-1/reference.png',
      publicUrl: 'https://cdn.example/pending/reference.png',
      previewUrl: 'https://signed.example/pending/reference.png',
      contentType: 'image/png',
      size: file.size,
    })

    await waitFor(() => expect(onChange).toHaveBeenCalledWith({ upload_session_id: 'session-1' }))
    expect(onChange).not.toHaveBeenCalledWith(expect.objectContaining({ preview_url: expect.anything() }))
    expect(onUploadingChange.mock.calls.map(([uploading]) => uploading)).toEqual([true, false])

    fireEvent.click(screen.getByRole('button', { name: '移除参考图' }))
    expect(onChange).toHaveBeenLastCalledWith(null)
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:reference-preview')
  })

  it('previews an existing asset from its signed download URL without creating a blob', () => {
    render(
      <ReferenceAssetUpload
        value={{
          asset_id: 'asset-1',
          file_name: 'reference.png',
          content_type: 'image/png',
          size: 9,
          download_url: 'https://signed.example/assets/asset-1',
          download_expires_at: '2026-07-17T09:15:00Z',
        }}
        purpose="task_reference"
        onChange={vi.fn()}
      />,
    )

    expect(screen.getByAltText('参考图')).toHaveAttribute(
      'src',
      'https://signed.example/assets/asset-1',
    )
    expect(createObjectURL).not.toHaveBeenCalled()
  })

  it('revokes a component-local blob preview on unmount', async () => {
    vi.mocked(uploadToOSS).mockResolvedValueOnce({
      uploadId: 'transport-2',
      uploadSessionId: 'session-2',
      key: 'uploads/pending/user/session-2/reference.png',
      publicUrl: 'https://cdn.example/pending/reference.png',
      previewUrl: 'https://signed.example/pending/reference.png',
      contentType: 'image/png',
      size: 9,
    })
    const { unmount } = render(
      <ReferenceAssetUpload
        value={null}
        purpose="task_reference"
        onChange={vi.fn()}
      />,
    )

    fireEvent.change(screen.getByLabelText('参考图文件'), {
      target: { files: [new File(['reference'], 'reference.png', { type: 'image/png' })] },
    })
    await waitFor(() => expect(screen.getByAltText('参考图')).toBeInTheDocument())

    unmount()

    expect(revokeObjectURL).toHaveBeenCalledWith('blob:reference-preview')
  })

  it('revokes the previous blob when a completed upload is replaced', async () => {
    createObjectURL
      .mockReturnValueOnce('blob:first-preview')
      .mockReturnValueOnce('blob:second-preview')
    vi.mocked(uploadToOSS)
      .mockResolvedValueOnce({
        uploadId: 'transport-1',
        uploadSessionId: 'session-1',
        key: 'uploads/pending/user/session-1/first.png',
        publicUrl: '',
        previewUrl: '',
        contentType: 'image/png',
        size: 5,
      })
      .mockResolvedValueOnce({
        uploadId: 'transport-2',
        uploadSessionId: 'session-2',
        key: 'uploads/pending/user/session-2/second.png',
        publicUrl: '',
        previewUrl: '',
        contentType: 'image/png',
        size: 6,
      })

    render(
      <ReferenceAssetUpload
        value={null}
        purpose="task_reference"
        onChange={vi.fn()}
      />,
    )
    const input = screen.getByLabelText('参考图文件')

    fireEvent.change(input, {
      target: { files: [new File(['first'], 'first.png', { type: 'image/png' })] },
    })
    await waitFor(() => expect(uploadToOSS).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(input).not.toBeDisabled())

    fireEvent.change(input, {
      target: { files: [new File(['second'], 'second.png', { type: 'image/png' })] },
    })

    expect(revokeObjectURL).toHaveBeenCalledWith('blob:first-preview')
    expect(screen.getByAltText('参考图')).toHaveAttribute('src', 'blob:second-preview')
  })

  it('aborts on unmount and ignores a late successful upload', async () => {
    const upload = deferred<Awaited<ReturnType<typeof uploadToOSS>>>()
    vi.mocked(uploadToOSS).mockReturnValueOnce(upload.promise)
    const onChange = vi.fn()
    const onUploadingChange = vi.fn()
    const { unmount } = render(
      <ReferenceAssetUpload
        value={null}
        purpose="task_reference"
        onChange={onChange}
        onUploadingChange={onUploadingChange}
      />,
    )

    fireEvent.change(screen.getByLabelText('参考图文件'), {
      target: { files: [new File(['reference'], 'reference.png', { type: 'image/png' })] },
    })
    await waitFor(() => expect(uploadToOSS).toHaveBeenCalledTimes(1))
    const signal = vi.mocked(uploadToOSS).mock.calls[0][0].signal

    unmount()

    expect(signal?.aborted).toBe(true)
    expect(onUploadingChange.mock.calls.map(([uploading]) => uploading)).toEqual([true, false])

    await act(async () => {
      upload.resolve({
        uploadId: 'transport-late',
        uploadSessionId: 'session-late',
        key: 'uploads/pending/user/session-late/reference.png',
        publicUrl: '',
        previewUrl: '',
        contentType: 'image/png',
        size: 9,
      })
    })

    expect(onChange).not.toHaveBeenCalled()
    expect(onUploadingChange.mock.calls.map(([uploading]) => uploading)).toEqual([true, false])
  })

  it('aborts and ignores late completion when the controlled value is replaced', async () => {
    const upload = deferred<Awaited<ReturnType<typeof uploadToOSS>>>()
    vi.mocked(uploadToOSS).mockReturnValueOnce(upload.promise)
    const onChange = vi.fn()
    const onUploadingChange = vi.fn()
    const { rerender } = render(
      <ReferenceAssetUpload
        value={null}
        purpose="project_reference"
        onChange={onChange}
        onUploadingChange={onUploadingChange}
      />,
    )

    fireEvent.change(screen.getByLabelText('参考图文件'), {
      target: { files: [new File(['candidate'], 'candidate.png', { type: 'image/png' })] },
    })
    await waitFor(() => expect(uploadToOSS).toHaveBeenCalledTimes(1))
    const signal = vi.mocked(uploadToOSS).mock.calls[0][0].signal

    rerender(
      <ReferenceAssetUpload
        value={{
          asset_id: 'asset-external',
          file_name: 'external.png',
          content_type: 'image/png',
          size: 12,
          download_url: 'https://signed.example/assets/external',
          download_expires_at: '2026-07-17T09:15:00Z',
        }}
        purpose="project_reference"
        onChange={onChange}
        onUploadingChange={onUploadingChange}
      />,
    )

    expect(signal?.aborted).toBe(true)
    expect(screen.getByAltText('参考图')).toHaveAttribute('src', 'https://signed.example/assets/external')
    expect(onUploadingChange.mock.calls.map(([uploading]) => uploading)).toEqual([true, false])

    await act(async () => {
      upload.resolve({
        uploadId: 'transport-stale',
        uploadSessionId: 'session-stale',
        key: 'uploads/pending/user/session-stale/candidate.png',
        publicUrl: '',
        previewUrl: '',
        contentType: 'image/png',
        size: 9,
      })
    })

    expect(onChange).not.toHaveBeenCalled()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(onUploadingChange.mock.calls.map(([uploading]) => uploading)).toEqual([true, false])
  })

  it('uses a native button for the empty upload trigger', () => {
    render(
      <ReferenceAssetUpload
        value={null}
        purpose="task_reference"
        onChange={vi.fn()}
      />,
    )

    expect(screen.getByRole('button', { name: '上传参考图' }).tagName).toBe('BUTTON')
  })

  it('announces upload errors', async () => {
    vi.mocked(uploadToOSS).mockRejectedValueOnce(new Error('网络不可用'))
    render(
      <ReferenceAssetUpload
        value={null}
        purpose="task_reference"
        onChange={vi.fn()}
      />,
    )

    fireEvent.change(screen.getByLabelText('参考图文件'), {
      target: { files: [new File(['reference'], 'reference.png', { type: 'image/png' })] },
    })

    expect(await screen.findByRole('alert')).toHaveTextContent('网络不可用')
  })
})
