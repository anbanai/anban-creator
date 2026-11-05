import { fireEvent, screen, waitFor } from '@testing-library/react'
import { render } from '@/test/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { TaskFileDownloads, TaskStageFiles } from './TaskStageFiles'
import { api } from '@/lib/api'
import type { TaskFile } from '@/types'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      tasks: {
        ...actual.api.tasks,
        previewFileBlob: vi.fn(),
        fetchPreviewHTML: vi.fn(),
        downloadFileBlob: vi.fn(),
      },
    },
  }
})

const files = [
  { id: 'delivered', task_id: 'task-1', state: 'delivered' as const, role: 'content', file_name: 'final.md', mime_type: 'text/markdown', file_size: 10, url: '', is_deliverable: true, created_at: '' },
  { id: 'retained', task_id: 'task-1', state: 'retained' as const, role: 'content', file_name: 'draft.md', mime_type: 'text/markdown', file_size: 10, url: '', created_at: '' },
]

function articleFiles(imageURL: string): TaskFile[] {
  return [
    {
      id: 'article-html', task_id: 'task-1', state: 'delivered', role: 'html', file_name: '05-article.html', mime_type: 'text/html',
      file_size: 10, url: '/api/v1/files/05-article.html', preview_url: '/api/v1/tasks/task-1/files/article-html/preview', is_deliverable: true, created_at: '',
    },
    {
      id: 'article-image', task_id: 'task-1', state: 'delivered', role: 'image', file_name: 'img_01.png', mime_type: 'image/png',
      file_size: 3, url: '/api/v1/files/img_01.png', preview_url: 'https://creator.anbanai.com/api/v1/tasks/task-1/files/article-image/preview',
      platform_image_url: imageURL, is_deliverable: true, created_at: '',
    },
  ]
}

function stubImageLoader() {
  vi.stubGlobal('Image', class {
    onload: (() => void) | null = null
    onerror: (() => void) | null = null
    set src(_value: string) { queueMicrotask(() => this.onload?.()) }
  })
}

function stubRichClipboard() {
  const write = vi.fn().mockResolvedValue(undefined)
  vi.stubGlobal('ClipboardItem', class { constructor(readonly data: Record<string, Blob>) {} })
  stubImageLoader()
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { write } })
  return write
}

async function copiedArticleHTML(write: ReturnType<typeof vi.fn>): Promise<string> {
  const item = (write.mock.calls[0][0] as unknown as Array<{ data: Record<string, Blob | Promise<Blob>> }>)[0]
  return (await item.data['text/html']).text()
}

describe('TaskStageFiles', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('copies the article with every delivered image from the complete file set', async () => {
    const imageURL = 'https://creator.anbanai.com/api/v1/article-assets/2b7c1d9e-4f6a-4c2b-9f70-8d5e3a1b0c42'
    vi.mocked(api.tasks.previewFileBlob).mockResolvedValue(new Blob([`<h1>标题</h1><p>正文</p><img src="${imageURL}">`], { type: 'text/html' }))
    const write = stubRichClipboard()

    render(<TaskStageFiles files={articleFiles(imageURL)} taskId="task-1" taskType="wechat-article" />)
    fireEvent.click(screen.getByRole('button', { name: '预览 05-article.html' }))
    fireEvent.click(await screen.findByRole('button', { name: '复制正文（含图片）' }))

    await waitFor(() => expect(write).toHaveBeenCalledOnce())
    await expect(copiedArticleHTML(write)).resolves.toContain(`src="${imageURL}"`)
  })

  it('resolves article images from the complete task file set when a stage only lists the document', async () => {
    const imageURL = 'https://creator.anbanai.com/api/v1/article-assets/stage-image'
    vi.mocked(api.tasks.previewFileBlob).mockResolvedValue(new Blob([`<h1>标题</h1><img src="${imageURL}">`], { type: 'text/html' }))
    const write = stubRichClipboard()
    const [html, image] = articleFiles(imageURL)

    render(<TaskStageFiles files={[html]} relatedFiles={[html, image]} taskId="task-1" taskType="wechat-article" />)
    fireEvent.click(screen.getByRole('button', { name: '预览 05-article.html' }))
    fireEvent.click(await screen.findByRole('button', { name: '复制正文（含图片）' }))

    await waitFor(() => expect(write).toHaveBeenCalledOnce())
    await expect(copiedArticleHTML(write)).resolves.toContain(`src="${imageURL}"`)
  })

  it('downloads the article with every delivered image from the complete file set', async () => {
    const imageURL = 'https://creator.anbanai.com/api/v1/article-assets/download-image'
    vi.mocked(api.tasks.previewFileBlob).mockResolvedValue(new Blob([`<h1>标题</h1><img src="${imageURL}">`], { type: 'text/html' }))
    stubImageLoader()
    const createObjectURL = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:article')
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    const [html, image] = articleFiles(imageURL)

    render(<TaskStageFiles files={[html]} relatedFiles={[html, image]} taskId="task-1" taskType="wechat-article" />)
    fireEvent.click(screen.getByRole('button', { name: '预览 05-article.html' }))
    fireEvent.click(await screen.findByRole('button', { name: '下载含图片 HTML' }))

    await waitFor(() => expect(createObjectURL).toHaveBeenCalledOnce())
    expect(await (createObjectURL.mock.calls[0][0] as Blob).text()).toContain(`src="${imageURL}"`)
    expect(click).toHaveBeenCalledOnce()
  })
})

describe('TaskFileDownloads', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('offers one download icon and lets users choose the artifact set', async () => {
    render(<TaskFileDownloads taskId="task-1" files={files} />)

    const downloadButton = screen.getByRole('button', { name: '下载产物' })
    expect(downloadButton).toHaveClass('size-11')
    expect(downloadButton).toHaveClass('hover:bg-transparent')
    fireEvent.click(downloadButton)

    expect(await screen.findByRole('menuitem', { name: '下载交付成果' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: '下载已保留产物' })).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: /下载/ })).toHaveLength(1)
  })

  it('keeps a single icon while a download is loading', async () => {
    vi.spyOn(api.tasks, 'downloadZipBlob').mockImplementation(() => new Promise(() => {}))
    render(<TaskFileDownloads taskId="task-1" files={[files[0]]} />)

    const downloadButton = screen.getByRole('button', { name: '下载交付成果 (ZIP)' })
    expect(downloadButton.querySelectorAll('svg')).toHaveLength(1)
    fireEvent.click(downloadButton)
    await waitFor(() => expect(downloadButton).toHaveAttribute('aria-busy', 'true'))
    expect(downloadButton.querySelectorAll('svg')).toHaveLength(1)
  })

  it('shows one busy icon after choosing an artifact set from the download menu', async () => {
    vi.spyOn(api.tasks, 'downloadZipBlob').mockImplementation(() => new Promise(() => {}))
    render(<TaskFileDownloads taskId="task-1" files={files} />)

    fireEvent.click(screen.getByRole('button', { name: '下载产物' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: '下载交付成果' }))
    const downloadButton = screen.getByRole('button', { name: '下载产物' })
    await waitFor(() => expect(downloadButton).toHaveAttribute('aria-busy', 'true'))
    expect(downloadButton).toBeDisabled()
    expect(downloadButton.querySelectorAll('svg')).toHaveLength(1)
  })
})
