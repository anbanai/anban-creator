import { act, fireEvent, screen, waitFor } from '@testing-library/react'
import { useRef, useState } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { uploadsApi } from '@/lib/api/uploads'
import {
  downloadBlob,
  isDesktop,
  saveUrlToFile,
} from '@/lib/tauri'
import { render } from '@/test/test-utils'
import type { InputAttachment, PromptAttachment } from '@/types/input-attachment'
import {
  AttachmentPreviewDialog,
  MAX_TEXT_PREVIEW_BYTES,
} from './AttachmentPreviewDialog'

vi.mock('@/lib/api/uploads', () => ({
  uploadsApi: { resolveDownloadUrl: vi.fn() },
}))

vi.mock('@/lib/tauri', () => ({
  downloadBlob: vi.fn(),
  isDesktop: vi.fn(() => false),
  saveUrlToFile: vi.fn(),
}))

function attachment(overrides: Partial<PromptAttachment> = {}): PromptAttachment {
  return {
    id: 'attachment-1',
    type: 'image',
    fileName: 'reference.png',
    contentType: 'image/png',
    size: 12,
    status: 'uploaded',
    progress: 100,
    ...overrides,
  }
}

function renderDialog(props: Partial<React.ComponentProps<typeof AttachmentPreviewDialog>> = {}) {
  const onOpenChange = vi.fn()
  const result = render(
    <AttachmentPreviewDialog
      open
      onOpenChange={onOpenChange}
      attachments={[attachment()]}
      {...props}
    />,
  )
  return { ...result, onOpenChange }
}

describe('AttachmentPreviewDialog', () => {
  beforeEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
    vi.restoreAllMocks()
    vi.mocked(uploadsApi.resolveDownloadUrl).mockReset()
    vi.mocked(isDesktop).mockReset()
    vi.mocked(saveUrlToFile).mockReset()
    vi.mocked(downloadBlob).mockReset()
    vi.mocked(isDesktop).mockReturnValue(false)
    vi.mocked(saveUrlToFile).mockResolvedValue(false)
    vi.mocked(downloadBlob).mockResolvedValue(undefined)
  })

  it('renders a local image without resolving a remote URL or mutating serialized state', async () => {
    const file = new File(['pixels'], 'local.png', { type: 'image/png' })
    const item = attachment({ file, fileName: file.name, size: file.size })
    const value = { prompt: 'draw', attachments: [item] }
    const previewSource = vi.fn(() => 'blob:local-image')

    renderDialog({ attachments: undefined, value, previewSource })

    expect(await screen.findByRole('img', { name: 'local.png' })).toHaveAttribute('src', 'blob:local-image')
    expect(previewSource).toHaveBeenCalledWith('attachment-1')
    expect(uploadsApi.resolveDownloadUrl).not.toHaveBeenCalled()
    expect(JSON.stringify(value)).not.toContain('blob:local-image')
  })

  it('resolves upload identity exactly and reuses a valid signed URL across close and reopen', async () => {
    const onOpenChange = vi.fn()
    const resolve = vi.mocked(uploadsApi.resolveDownloadUrl)
    resolve.mockResolvedValue({
      url: 'https://oss.example.com/reference.png?signature=secret',
      expires_at: new Date(Date.now() + 120_000).toISOString(),
    })
    const item = attachment({ uploadId: 'upload-1', key: 'pending/user/upload-1/reference.png' })
    const { rerender } = render(
      <AttachmentPreviewDialog open onOpenChange={onOpenChange} attachments={[item]} />,
    )

    expect(await screen.findByRole('img', { name: 'reference.png' })).toHaveAttribute(
      'src',
      'https://oss.example.com/reference.png?signature=secret',
    )
    expect(resolve).toHaveBeenCalledWith({ upload_id: 'upload-1', key: item.key })
    expect(resolve).toHaveBeenCalledOnce()

    rerender(<AttachmentPreviewDialog open={false} onOpenChange={onOpenChange} attachments={[item]} />)
    rerender(<AttachmentPreviewDialog open onOpenChange={onOpenChange} attachments={[item]} />)
    await screen.findByRole('img', { name: 'reference.png' })
    expect(resolve).toHaveBeenCalledOnce()
    expect(JSON.stringify(item)).not.toContain('signature=secret')
  })

  it('deduplicates a pending resolution and refreshes cache entries inside the 30 second safety window', async () => {
    const now = new Date('2026-07-16T00:00:00Z')
    const nowSpy = vi.spyOn(Date, 'now').mockReturnValue(now.getTime())
    let settle!: (value: { url: string; expires_at: string }) => void
    const pending = new Promise<{ url: string; expires_at: string }>((resolve) => { settle = resolve })
    const resolve = vi.mocked(uploadsApi.resolveDownloadUrl)
      .mockReturnValueOnce(pending)
      .mockResolvedValueOnce({
        url: 'https://oss.example.com/fresh.png',
        expires_at: new Date(now.getTime() + 180_000).toISOString(),
      })
    const item = attachment({ uploadId: 'upload-1', key: 'pending/reference.png' })
    const onOpenChange = vi.fn()
    const { rerender } = render(
      <AttachmentPreviewDialog open onOpenChange={onOpenChange} attachments={[item]} />,
    )
    rerender(<AttachmentPreviewDialog open={false} onOpenChange={onOpenChange} attachments={[item]} />)
    rerender(<AttachmentPreviewDialog open onOpenChange={onOpenChange} attachments={[item]} />)
    expect(resolve).toHaveBeenCalledOnce()

    await act(async () => {
      settle({
        url: 'https://oss.example.com/short.png',
        expires_at: new Date(now.getTime() + 31_000).toISOString(),
      })
      await pending
    })
    await screen.findByRole('img', { name: 'reference.png' })

    nowSpy.mockReturnValue(now.getTime() + 2_000)
    rerender(<AttachmentPreviewDialog open={false} onOpenChange={onOpenChange} attachments={[item]} />)
    rerender(<AttachmentPreviewDialog open onOpenChange={onOpenChange} attachments={[item]} />)
    await waitFor(() => expect(resolve).toHaveBeenCalledTimes(2))
  })

  it('uses owner authorization for inherited key-only sources and shows resolver failures', async () => {
    const source: InputAttachment = {
      type: 'document',
      key: 'tasks/task-7/input/brief.pdf',
      file_name: 'brief.pdf',
      content_type: 'application/pdf',
      size: 100,
    }
    vi.mocked(uploadsApi.resolveDownloadUrl).mockRejectedValue(new Error('无权访问附件'))

    renderDialog({
      attachments: [attachment({ type: 'document', fileName: 'brief.pdf', contentType: 'application/pdf', key: source.key })],
      sourceAttachment: () => ({ ...source }),
      owner: { ownerType: 'task', ownerId: 'task-7' },
    })

    expect(await screen.findByRole('alert')).toHaveTextContent('无权访问附件')
    expect(uploadsApi.resolveDownloadUrl).toHaveBeenCalledWith({
      key: source.key,
      owner_type: 'task',
      owner_id: 'task-7',
    })
  })

  it.each([
    ['video', 'clip.mp4', 'video/mp4', 'video'],
    ['audio', 'voice.mp3', 'audio/mpeg', 'audio'],
    ['document', 'brief.pdf', 'application/pdf', 'object'],
  ] as const)('renders %s media directly from its authorized URL', async (type, fileName, contentType, selector) => {
    vi.mocked(uploadsApi.resolveDownloadUrl).mockResolvedValue({
      url: `https://oss.example.com/${fileName}`,
      expires_at: new Date(Date.now() + 120_000).toISOString(),
    })
    renderDialog({
      attachments: [attachment({ type, fileName, contentType, uploadId: 'upload-1', key: `pending/${fileName}` })],
    })

    await waitFor(() => expect(document.querySelector(selector)).toBeInTheDocument())
    const media = document.querySelector(selector) as HTMLElement
    if (selector === 'object') {
      expect(media).toHaveAttribute('data', `https://oss.example.com/${fileName}`)
    } else {
      expect(media).toHaveAttribute('src', `https://oss.example.com/${fileName}`)
    }
    if (selector === 'video' || selector === 'audio') expect(media).toHaveAttribute('preload', 'metadata')
    if (selector === 'video') expect(media).toHaveAttribute('playsinline')
  })

  it('renders inline text and JSON as inert plain text, with declared-large fallback', async () => {
    const inline = '<script>window.bad = true</script>\n# not rendered markdown'
    const sourceAttachment = vi.fn((id: string): InputAttachment | undefined => {
      if (id === 'text') return { type: 'text', text: inline, file_name: 'notes.md', size: inline.length }
      if (id === 'json-mime') return { type: 'document', text: '{"mime":true}', file_name: 'payload', content_type: 'Application/JSON; charset=utf-8', size: 13 }
      return undefined
    })
    const items = [
      attachment({ id: 'text', type: 'text', fileName: 'notes.md', contentType: 'text/markdown', size: inline.length }),
      attachment({ id: 'json', type: 'document', fileName: 'data.json', contentType: 'application/json', size: 12, file: new File(['{"ok":true}'], 'data.json', { type: 'application/json' }) }),
      attachment({ id: 'json-mime', type: 'document', fileName: 'payload', contentType: 'Application/JSON; charset=utf-8', size: 13 }),
      attachment({ id: 'large', type: 'text', fileName: 'large.txt', contentType: 'text/plain', size: MAX_TEXT_PREVIEW_BYTES + 1, file: new File(['small'], 'large.txt', { type: 'text/plain' }) }),
    ]
    const onSelectedChange = vi.fn()
    const { rerender } = renderDialog({ attachments: items, selectedId: 'text', sourceAttachment, onSelectedChange })

    const preview = await screen.findByTestId('attachment-text-preview')
    expect(preview.textContent).toBe(inline)
    expect(preview.querySelector('script')).toBeNull()

    rerender(<AttachmentPreviewDialog open onOpenChange={vi.fn()} attachments={items} selectedId="json" sourceAttachment={sourceAttachment} />)
    expect(await screen.findByTestId('attachment-text-preview')).toHaveTextContent('{"ok":true}')

    rerender(<AttachmentPreviewDialog open onOpenChange={vi.fn()} attachments={items} selectedId="json-mime" sourceAttachment={sourceAttachment} />)
    expect(await screen.findByTestId('attachment-text-preview')).toHaveTextContent('{"mime":true}')

    rerender(<AttachmentPreviewDialog open onOpenChange={vi.fn()} attachments={items} selectedId="large" sourceAttachment={sourceAttachment} />)
    expect(await screen.findByRole('alert')).toHaveTextContent('文件过大，无法在线预览')

  })

  it('enforces the text limit against actual UTF-8 bytes for inline and local files', async () => {
    const oversizedUtf8 = '界'.repeat(Math.floor(MAX_TEXT_PREVIEW_BYTES / 3) + 1)
    const oversizedFile = new File(
      [new Uint8Array(MAX_TEXT_PREVIEW_BYTES + 1)],
      'oversized.txt',
      { type: 'text/plain' },
    )
    const inline = attachment({
      id: 'inline-oversized',
      type: 'text',
      fileName: 'inline.txt',
      contentType: 'text/plain',
      size: 1,
    })
    const local = attachment({
      id: 'local-oversized',
      type: 'text',
      file: oversizedFile,
      fileName: oversizedFile.name,
      contentType: oversizedFile.type,
      size: 1,
    })
    const sourceAttachment = (id: string): InputAttachment | undefined => id === inline.id
      ? { type: 'text', text: oversizedUtf8, file_name: inline.fileName, size: 1 }
      : undefined
    const { rerender } = renderDialog({ attachments: [inline, local], selectedId: inline.id, sourceAttachment })

    expect(await screen.findByRole('alert')).toHaveTextContent('文件过大，无法在线预览')
    expect(screen.queryByTestId('attachment-text-preview')).not.toBeInTheDocument()

    rerender(
      <AttachmentPreviewDialog
        open
        onOpenChange={vi.fn()}
        attachments={[inline, local]}
        selectedId={local.id}
        sourceAttachment={sourceAttachment}
      />,
    )
    expect(await screen.findByRole('alert')).toHaveTextContent('文件过大，无法在线预览')
    expect(screen.queryByTestId('attachment-text-preview')).not.toBeInTheDocument()
  })

  it('streams remote text with a hard cap even when declared size is missing or wrong', async () => {
    const small = attachment({
      id: 'remote-small',
      type: 'text',
      fileName: 'small.txt',
      contentType: 'text/plain',
      size: 0,
      uploadId: 'upload-small',
      key: 'pending/small.txt',
    })
    const oversized = attachment({
      id: 'remote-oversized',
      type: 'text',
      fileName: 'oversized.txt',
      contentType: 'text/plain',
      size: 1,
      uploadId: 'upload-oversized',
      key: 'pending/oversized.txt',
    })
    const cancel = vi.fn()
    vi.mocked(uploadsApi.resolveDownloadUrl)
      .mockResolvedValueOnce({ url: 'https://oss.example.com/small.txt', expires_at: new Date(Date.now() + 120_000).toISOString() })
      .mockResolvedValueOnce({ url: 'https://oss.example.com/oversized.txt', expires_at: new Date(Date.now() + 120_000).toISOString() })
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(new Response('small remote text'))
      .mockResolvedValueOnce(new Response(new ReadableStream<Uint8Array>({
        start(streamController) {
          streamController.enqueue(new Uint8Array(MAX_TEXT_PREVIEW_BYTES))
          streamController.enqueue(new Uint8Array([1]))
        },
        cancel,
      }))))
    const { rerender } = renderDialog({ attachments: [small, oversized], selectedId: small.id })

    expect(await screen.findByTestId('attachment-text-preview')).toHaveTextContent('small remote text')

    rerender(<AttachmentPreviewDialog open onOpenChange={vi.fn()} attachments={[small, oversized]} selectedId={oversized.id} />)
    expect(await screen.findByRole('alert')).toHaveTextContent('文件过大，无法在线预览')
    expect(screen.queryByTestId('attachment-text-preview')).not.toBeInTheDocument()
    expect(cancel).toHaveBeenCalledOnce()
  })

  it('refreshes an authenticated remote text request once and aborts it when selection changes', async () => {
    vi.mocked(uploadsApi.resolveDownloadUrl)
      .mockResolvedValueOnce({ url: 'https://oss.example.com/expired.txt', expires_at: new Date(Date.now() + 120_000).toISOString() })
      .mockResolvedValueOnce({ url: 'https://oss.example.com/fresh.txt', expires_at: new Date(Date.now() + 120_000).toISOString() })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(null, { status: 403 }))
      .mockResolvedValueOnce(new Response('fresh authorized text'))
    vi.stubGlobal('fetch', fetchMock)
    const remote = attachment({
      id: 'remote-text',
      type: 'text',
      fileName: 'remote.txt',
      contentType: 'text/plain',
      size: 24,
      uploadId: 'upload-text',
      key: 'pending/remote.txt',
    })
    const inline = attachment({ id: 'inline', type: 'text', fileName: 'inline.txt', contentType: 'text/plain', size: 6 })
    const sourceAttachment = (id: string): InputAttachment | undefined => (
      id === 'inline' ? { type: 'text', text: 'inline', file_name: 'inline.txt', size: 6 } : undefined
    )
    const { rerender } = renderDialog({ attachments: [remote, inline], selectedId: remote.id, sourceAttachment })

    expect(await screen.findByTestId('attachment-text-preview')).toHaveTextContent('fresh authorized text')
    expect(uploadsApi.resolveDownloadUrl).toHaveBeenCalledTimes(2)
    expect(fetchMock).toHaveBeenCalledTimes(2)

    let observedSignal: AbortSignal | undefined
    fetchMock.mockImplementationOnce((_url, init?: RequestInit) => {
      observedSignal = init?.signal ?? undefined
      return new Promise(() => {})
    })
    vi.mocked(uploadsApi.resolveDownloadUrl).mockResolvedValueOnce({
      url: 'https://oss.example.com/another.txt',
      expires_at: new Date(Date.now() + 120_000).toISOString(),
    })
    const another = { ...remote, id: 'another', uploadId: 'upload-another', key: 'pending/another.txt' }
    rerender(<AttachmentPreviewDialog open onOpenChange={vi.fn()} attachments={[another, inline]} selectedId="another" sourceAttachment={sourceAttachment} />)
    await waitFor(() => expect(observedSignal).toBeDefined())
    rerender(<AttachmentPreviewDialog open onOpenChange={vi.fn()} attachments={[another, inline]} selectedId="inline" sourceAttachment={sourceAttachment} />)
    expect(observedSignal?.aborted).toBe(true)
  })

  it.each([
    ['document', 'report.docx', 'application/vnd.openxmlformats-officedocument.wordprocessingml.document'],
    ['document', 'archive.bin', 'application/octet-stream'],
  ] as const)('uses metadata and download fallback for %s %s', async (type, fileName, contentType) => {
    renderDialog({ attachments: [attachment({ type, fileName, contentType })] })
    expect(await screen.findByText('此文件类型不支持在线预览')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: `下载 ${fileName}` })).toBeInTheDocument()
  })

  it('navigates with arrow keys within bounds and ignores editable controls', async () => {
    const items = [
      attachment({ id: 'one', fileName: 'one.png', file: new File(['1'], 'one.png', { type: 'image/png' }) }),
      attachment({ id: 'two', fileName: 'two.png', file: new File(['2'], 'two.png', { type: 'image/png' }) }),
    ]
    const onSelectedChange = vi.fn()
    renderDialog({ attachments: items, selectedId: 'one', previewSource: (id) => `blob:${id}`, onSelectedChange })
    await screen.findByRole('img', { name: 'one.png' })

    fireEvent.keyDown(document, { key: 'ArrowLeft' })
    expect(onSelectedChange).not.toHaveBeenCalled()
    fireEvent.keyDown(document, { key: 'ArrowRight' })
    expect(onSelectedChange).toHaveBeenCalledWith('two', 1)

    const slider = screen.getByRole('group', { name: '缩放比例' })
    fireEvent.keyDown(slider, { key: 'ArrowRight' })
    expect(onSelectedChange).toHaveBeenCalledOnce()
  })

  it('downloads local files and remote files through web and desktop helpers', async () => {
    const local = new File(['local'], 'local.txt', { type: 'text/plain' })
    const { rerender } = renderDialog({
      attachments: [attachment({ type: 'text', fileName: 'local.txt', contentType: 'text/plain', size: local.size, file: local })],
      previewSource: () => 'blob:local',
    })
    fireEvent.click(await screen.findByRole('button', { name: '下载 local.txt' }))
    await waitFor(() => expect(downloadBlob).toHaveBeenCalledWith('local.txt', local))

    vi.mocked(uploadsApi.resolveDownloadUrl).mockResolvedValue({
      url: 'https://oss.example.com/remote.png',
      expires_at: new Date(Date.now() + 120_000).toISOString(),
    })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, blob: () => Promise.resolve(new Blob(['remote'])) }))
    const remote = attachment({ fileName: 'remote.png', uploadId: 'upload-2', key: 'pending/remote.png' })
    rerender(<AttachmentPreviewDialog open onOpenChange={vi.fn()} attachments={[remote]} />)
    fireEvent.click(await screen.findByRole('button', { name: '下载 remote.png' }))
    await waitFor(() => expect(downloadBlob).toHaveBeenCalledWith('remote.png', expect.any(Blob)))

    vi.mocked(isDesktop).mockReturnValue(true)
    vi.mocked(saveUrlToFile).mockResolvedValue(true)
    fireEvent.click(screen.getByRole('button', { name: '下载 remote.png' }))
    await waitFor(() => expect(saveUrlToFile).toHaveBeenCalledWith('https://oss.example.com/remote.png', 'remote.png'))
  })

  it('refreshes a failed signed renderer once and exposes the second failure without a third call', async () => {
    vi.mocked(uploadsApi.resolveDownloadUrl)
      .mockResolvedValueOnce({ url: 'https://oss.example.com/old.png', expires_at: new Date(Date.now() + 120_000).toISOString() })
      .mockResolvedValueOnce({ url: 'https://oss.example.com/new.png', expires_at: new Date(Date.now() + 120_000).toISOString() })
    renderDialog({ attachments: [attachment({ uploadId: 'upload-1', key: 'pending/reference.png' })] })

    fireEvent.error(await screen.findByRole('img', { name: 'reference.png' }))
    const refreshed = await screen.findByRole('img', { name: 'reference.png' })
    expect(refreshed).toHaveAttribute('src', 'https://oss.example.com/new.png')
    fireEvent.error(refreshed)
    expect(await screen.findByRole('alert')).toHaveTextContent('预览加载失败')
    expect(uploadsApi.resolveDownloadUrl).toHaveBeenCalledTimes(2)
  })

  it('falls back from native and network errors to refresh a download once', async () => {
    vi.mocked(isDesktop).mockReturnValue(true)
    vi.mocked(saveUrlToFile).mockRejectedValue(new Error('native download failed'))
    vi.mocked(uploadsApi.resolveDownloadUrl)
      .mockResolvedValueOnce({ url: 'https://oss.example.com/expired.png', expires_at: new Date(Date.now() + 120_000).toISOString() })
      .mockResolvedValueOnce({ url: 'https://oss.example.com/fresh.png', expires_at: new Date(Date.now() + 120_000).toISOString() })
    vi.stubGlobal('fetch', vi.fn()
      .mockRejectedValueOnce(new TypeError('network request failed'))
      .mockResolvedValueOnce({ ok: true, status: 200, blob: () => Promise.resolve(new Blob(['fresh'])) }))
    renderDialog({ attachments: [attachment({ uploadId: 'upload-1', key: 'pending/reference.png' })] })

    fireEvent.click(await screen.findByRole('button', { name: '下载 reference.png' }))

    await waitFor(() => expect(downloadBlob).toHaveBeenCalledWith('reference.png', expect.any(Blob)))
    expect(uploadsApi.resolveDownloadUrl).toHaveBeenCalledTimes(2)
    expect(saveUrlToFile).toHaveBeenCalledTimes(2)
  })

  it('ignores a stale renderer refresh after the selected attachment changes', async () => {
    let settleRefresh!: (value: { url: string; expires_at: string }) => void
    const pendingRefresh = new Promise<{ url: string; expires_at: string }>((resolve) => { settleRefresh = resolve })
    vi.mocked(uploadsApi.resolveDownloadUrl)
      .mockResolvedValueOnce({ url: 'https://oss.example.com/old.png', expires_at: new Date(Date.now() + 120_000).toISOString() })
      .mockReturnValueOnce(pendingRefresh)
    const remote = attachment({ id: 'remote', uploadId: 'upload-1', key: 'pending/remote.png' })
    const localFile = new File(['local'], 'local.png', { type: 'image/png' })
    const local = attachment({ id: 'local', file: localFile, fileName: localFile.name, size: localFile.size })
    const { rerender } = renderDialog({
      attachments: [remote, local],
      selectedId: remote.id,
      previewSource: (id) => id === local.id ? 'blob:local' : undefined,
    })
    fireEvent.error(await screen.findByRole('img', { name: 'reference.png' }))

    rerender(
      <AttachmentPreviewDialog
        open
        onOpenChange={vi.fn()}
        attachments={[remote, local]}
        selectedId={local.id}
        previewSource={(id) => id === local.id ? 'blob:local' : undefined}
      />,
    )
    expect(await screen.findByRole('img', { name: 'local.png' })).toHaveAttribute('src', 'blob:local')

    await act(async () => {
      settleRefresh({ url: 'https://oss.example.com/refreshed-old.png', expires_at: new Date(Date.now() + 120_000).toISOString() })
      await pendingRefresh
    })
    expect(screen.getByRole('img', { name: 'local.png' })).toHaveAttribute('src', 'blob:local')
  })

  it('closes on Escape and returns focus to the provided element', async () => {
    const onOpenChange = vi.fn()
    function Harness() {
      const [open, setOpen] = useState(true)
      const triggerRef = useRef<HTMLButtonElement>(null)
      return (
        <>
          <button ref={triggerRef} type="button">打开预览</button>
          <AttachmentPreviewDialog
            open={open}
            onOpenChange={(nextOpen, eventDetails) => {
              onOpenChange(nextOpen, eventDetails)
              setOpen(nextOpen)
            }}
            attachments={[attachment()]}
            finalFocus={triggerRef}
          />
        </>
      )
    }
    render(<Harness />)

    await screen.findByRole('dialog')
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onOpenChange).toHaveBeenCalledWith(false, expect.anything())
    await waitFor(() => expect(screen.getByRole('button', { name: '打开预览' })).toHaveFocus())
  })
})
