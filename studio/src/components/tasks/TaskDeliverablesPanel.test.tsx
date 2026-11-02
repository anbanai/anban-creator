import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { http, HttpResponse } from 'msw'
import { server } from '@/test/mocks/server'
import { TaskDeliverablesPanel } from './TaskDeliverablesPanel'
import type { TaskFile, TaskOutcome } from '@/types'

const files = [
  { id: 'core', task_id: 'task-1', state: 'delivered' as const, role: 'content', file_name: 'final.md', mime_type: 'text/markdown', file_size: 10, url: '', is_deliverable: true, delivery_role: 'content', created_at: '' },
  { id: 'internal', task_id: 'task-1', state: 'delivered' as const, role: 'review', file_name: 'review.json', mime_type: 'application/json', file_size: 10, url: '', is_deliverable: false, producer_stage_id: 'review', created_at: '' },
]

describe('TaskDeliverablesPanel', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows and downloads only core delivery files to ordinary users', () => {
    render(<TaskDeliverablesPanel taskId="task-1" taskStatus="completed" files={files} isAdmin={false} />)

    expect(screen.getByText('final.md')).toBeInTheDocument()
    expect(screen.queryByText('review.json')).not.toBeInTheDocument()
    expect(screen.queryByText('内部文件')).not.toBeInTheDocument()
  })

  it('lets admins preview internal files in a separate grouped disclosure', () => {
    render(<TaskDeliverablesPanel taskId="task-1" taskStatus="completed" files={files} isAdmin />)

    expect(screen.getByText('final.md')).toBeInTheDocument()
    fireEvent.click(screen.getByText('内部文件 (1)'))
    expect(screen.getByText('review.json')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'review' })).toBeInTheDocument()
  })

  it('keeps sharing reachable after failed continuation but hides unsupported task kinds', () => {
    const { rerender } = render(<TaskDeliverablesPanel taskId="task-1" taskStatus="failed" taskType="seednote" taskKind="content_generation" files={files} isAdmin={false} />)
    expect(screen.getByRole('button', { name: '分享作品' })).toBeEnabled()
    rerender(<TaskDeliverablesPanel taskId="task-1" taskStatus="completed" taskType="seednote" taskKind="analysis" files={files} isAdmin={false} />)
    expect(screen.queryByRole('button', { name: '分享作品' })).not.toBeInTheDocument()
  })

  it('opens sharing beside the trigger without hiding the delivery page', async () => {
    server.use(http.get('/api/v1/tasks/task-1/share', () => HttpResponse.json({ data: { status: 'none' } })))
    render(<TaskDeliverablesPanel taskId="task-1" taskStatus="completed" taskType="seednote" files={files} isAdmin={false} />)
    fireEvent.click(screen.getByRole('button', { name: '分享作品' }))
    expect(await screen.findByRole('dialog', { name: '分享作品' })).toBeVisible()
    expect(screen.getByRole('heading', { name: '交付结果' })).toBeVisible()
    expect(screen.getByText('final.md')).toBeVisible()
    expect(screen.queryByRole('region', { name: '分享设置' })).not.toBeInTheDocument()
  })

  it.each(['awaiting_review', 'completed', 'failed', 'cancelled'] as const)('keeps logs available in the %s state', (taskStatus) => {
    const onOpenLogs = vi.fn()
    render(<TaskDeliverablesPanel taskId="task-1" taskStatus={taskStatus} files={files} isAdmin={false} onOpenLogs={onOpenLogs} />)

    fireEvent.click(screen.getByRole('button', { name: '查看完整日志' }))

    expect(onOpenLogs).toHaveBeenCalledOnce()
  })

  it('blocks the article image copy action when visual delivery is partial', async () => {
    server.use(http.get('/api/v1/tasks/task-1/files/article-html/preview', () => new HttpResponse(
      '<h1>标题</h1><img src="https://creator.anbanai.com/api/v1/article-assets/image-1">',
      { headers: { 'Content-Type': 'text/html' } },
    )))
    const articleFiles: TaskFile[] = [
      {
        id: 'article-html', task_id: 'task-1', state: 'delivered', role: 'html', file_name: '05-article.html', mime_type: 'text/html',
        file_size: 10, url: '', preview_url: '/api/v1/tasks/task-1/files/article-html/preview', is_deliverable: true, created_at: '',
      },
      {
        id: 'article-image', task_id: 'task-1', state: 'delivered', role: 'image', file_name: 'img_01.png', mime_type: 'image/png',
        file_size: 10, url: '', platform_image_url: 'https://creator.anbanai.com/api/v1/article-assets/image-1', is_deliverable: true, created_at: '',
      },
    ]
    const outcome: TaskOutcome = {
      core_delivery: { status: 'complete' },
      visual: { status: 'partial' },
      review: { status: 'passed' },
      publication: { status: 'not_requested' },
      warnings: [{ code: 'visual_partial', message: '请求的封面或正文配图未全部生成，现有图片已保留。' }],
    }
    render(<TaskDeliverablesPanel taskId="task-1" taskStatus="completed" taskType="wechat-article" files={articleFiles} outcome={outcome} isAdmin={false} />)

    fireEvent.click(screen.getByRole('button', { name: '预览 05-article.html' }))

    expect(await screen.findByTitle('文章预览')).toBeInTheDocument()
    expect(await screen.findByRole('alert')).toHaveTextContent('正文配图交付未通过')
    expect(screen.queryByRole('button', { name: '复制正文（含图片）' })).not.toBeInTheDocument()
  })

  it('resolves an article image outside the core delivery set when copying', async () => {
    const imageURL = 'https://creator.anbanai.com/api/v1/article-assets/internal-image'
    server.use(http.get('/api/v1/tasks/task-1/files/article-html/preview', () => new HttpResponse(
      `<h1>标题</h1><img src="${imageURL}">`,
      { headers: { 'Content-Type': 'text/html' } },
    )))
    const articleFiles: TaskFile[] = [
      {
        id: 'article-html', task_id: 'task-1', state: 'delivered', role: 'html', file_name: '05-article.html', mime_type: 'text/html',
        file_size: 10, url: '', preview_url: '/api/v1/tasks/task-1/files/article-html/preview', is_deliverable: true, created_at: '',
      },
      {
        id: 'article-image', task_id: 'task-1', state: 'delivered', role: 'image', file_name: 'img_01.png', mime_type: 'image/png',
        file_size: 10, url: '', platform_image_url: imageURL, is_deliverable: false, producer_stage_id: 'visual', created_at: '',
      },
    ]
    const write = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('ClipboardItem', class { constructor(readonly data: Record<string, Blob | Promise<Blob>>) {} })
    vi.stubGlobal('Image', class {
      onload: (() => void) | null = null
      onerror: (() => void) | null = null
      set src(_value: string) { queueMicrotask(() => this.onload?.()) }
    })
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { write } })

    render(<TaskDeliverablesPanel taskId="task-1" taskStatus="completed" taskType="wechat-article" files={articleFiles} isAdmin />)
    fireEvent.click(screen.getByRole('button', { name: '预览 05-article.html' }))
    fireEvent.click(await screen.findByRole('button', { name: '复制正文（含图片）' }))

    await waitFor(() => expect(write).toHaveBeenCalledOnce())
    const item = (write.mock.calls[0][0] as unknown as Array<{ data: Record<string, Blob | Promise<Blob>> }>)[0]
    expect(await (await item.data['text/html']).text()).toContain(`src="${imageURL}"`)
  })
})
