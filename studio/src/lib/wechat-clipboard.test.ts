import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { TaskFile } from '@/types'
import { articleMetadataValue, copyWechatArticle, downloadWechatArticleHTML, prepareWechatArticleHTML, prepareWechatArticleHTMLForDownload, sanitizeWechatHTML, writeWechatPlainText, writeWechatRichText } from './wechat-clipboard'

const imageFile: TaskFile = {
  id: 'img-1', task_id: 'task-1', state: 'delivered', role: 'image', file_name: 'inline.png', mime_type: 'image/png',
  file_size: 3, url: '/files/inline.png', platform_image_url: 'https://creator.anbanai.com/api/v1/article-assets/img-1', preview_url: '/api/tasks/task-1/files/img-1/preview', is_deliverable: true, created_at: '',
}

describe('WeChat clipboard', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.stubGlobal('Image', class {
      onload: (() => void) | null = null
      onerror: (() => void) | null = null
      set src(_value: string) { queueMicrotask(() => this.onload?.()) }
    })
  })

  it('sanitizes active content, unsafe CSS and dangerous links', () => {
    const result = sanitizeWechatHTML('<p onclick="run()" style="color:red;background-image:url(https://bad.invalid/x);position:fixed">Safe<script>bad()</script><img src="x" onerror="run()"><a href="data:text/html,bad">Link</a></p>')
    expect(result).toContain('color:red')
    expect(result).not.toContain('onclick')
    expect(result).not.toContain('script')
    expect(result).not.toContain('onerror')
    expect(result).not.toContain('background-image')
    expect(result).not.toContain('position:fixed')
    expect(result).not.toContain('href="data:')
  })

  it('preserves the durable public image URL in copied HTML', async () => {
    const html = await prepareWechatArticleHTML(`<p>Hello<img src="${imageFile.platform_image_url}"></p>`, [imageFile], async () => new Blob())
    expect(html).toContain(`src="${imageFile.platform_image_url}"`)
    expect(html).not.toContain('data:image')
  })

  it('rejects article images without a public HTTPS URL', async () => {
    const privateURLFile = { ...imageFile, platform_image_url: '/api/v1/tasks/task-1/files/img-1/preview' }
    await expect(prepareWechatArticleHTML('<img src="/api/v1/tasks/task-1/files/img-1/preview">', [privateURLFile], async () => new Blob())).rejects.toThrow('公开 HTTPS')
  })

  it('rejects copy when a public image cannot load', async () => {
    vi.stubGlobal('Image', class {
      onload: (() => void) | null = null
      onerror: (() => void) | null = null
      set src(_value: string) { queueMicrotask(() => this.onerror?.()) }
    })
    await expect(prepareWechatArticleHTML(`<img src="${imageFile.platform_image_url}">`, [imageFile], async () => new Blob())).rejects.toThrow('无法加载')
  })

  it('rejects oversized clipboard content but allows HTML download preparation', async () => {
    const source = `<p>${'x'.repeat(8 * 1024 * 1024)}</p>`
    await expect(prepareWechatArticleHTML(source, [], async () => new Blob())).rejects.toThrow('超过剪贴板容量')
    await expect(prepareWechatArticleHTMLForDownload(source, [], async () => new Blob())).resolves.toContain('x'.repeat(32))
  }, 60_000)

  it('requires a structured TaskFile reference for every image', async () => {
    await expect(prepareWechatArticleHTML('<img src="/guess.png">', [imageFile], async () => new Blob())).rejects.toThrow('找不到图片素材')
  })

  it('writes HTML and plain-text clipboard representations', async () => {
    const write = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('ClipboardItem', class { readonly types = ['text/html', 'text/plain']; constructor(readonly data: Record<string, Blob>) {} })
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { write } })
    await writeWechatRichText('<p><strong>Hello</strong></p>')
    expect(write).toHaveBeenCalledOnce()
    const item = (write.mock.calls[0][0] as unknown as ClipboardItem[])[0] as unknown as { data: Record<string, Blob> }
    expect(item.data).toHaveProperty('text/html')
    expect(item.data).toHaveProperty('text/plain')
  })

  it('starts rich clipboard write with the durable image URL', async () => {
    const write = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('ClipboardItem', class { constructor(readonly data: Record<string, Blob | Promise<Blob>>) {} })
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { write } })
    const copy = copyWechatArticle(`<img src="${imageFile.platform_image_url}">`, [imageFile], vi.fn())
    expect(write).toHaveBeenCalledOnce()
    await copy
    expect((write.mock.calls[0][0] as unknown as { 0: { data: Record<string, Blob | Promise<Blob>> } })[0].data['text/html']).toBeDefined()
  })

  it('falls back when a browser rejects the async ClipboardItem write', async () => {
    const write = vi.fn().mockRejectedValue(new Error('async clipboard unsupported'))
    vi.stubGlobal('ClipboardItem', class { constructor(readonly data: Record<string, Blob | Promise<Blob>>) {} })
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { write } })
    const execCommand = vi.fn().mockReturnValue(true)
    document.execCommand = execCommand
    await copyWechatArticle('<p>Fallback</p>', [], async () => new Blob())
    expect(execCommand).toHaveBeenCalledWith('copy')
  })

  it('uses the selection-based rich-text fallback when Clipboard API is unavailable', async () => {
    vi.stubGlobal('ClipboardItem', undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined })
    const execCommand = vi.fn().mockReturnValue(true)
    document.execCommand = execCommand
    await writeWechatRichText('<p>Fallback</p>')
    expect(execCommand).toHaveBeenCalledWith('copy')
    expect(document.querySelector('[contenteditable="true"]')).toBeNull()
  })

  it('falls back to execCommand when plain-text Clipboard API writes are rejected', async () => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: vi.fn().mockRejectedValue(new Error('permission denied')) } })
    const execCommand = vi.fn().mockReturnValue(true)
    document.execCommand = execCommand
    await writeWechatPlainText('Fallback')
    expect(execCommand).toHaveBeenCalledWith('copy')
  })

  it('prefers draft author metadata and falls back to the task snapshot', () => {
    expect(articleMetadataValue({ title: '', digest: '', author: 'Draft Author' }, 'author', 'Snapshot Author')).toBe('Draft Author')
    expect(articleMetadataValue({ title: '', digest: '', author: '' }, 'author', 'Snapshot Author')).toBe('Snapshot Author')
  })

  it('downloads sanitized HTML as a file', () => {
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:test')
    const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    downloadWechatArticleHTML('<p onclick="x()">Safe</p>')
    expect(click).toHaveBeenCalledOnce()
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:test')
  })
})
