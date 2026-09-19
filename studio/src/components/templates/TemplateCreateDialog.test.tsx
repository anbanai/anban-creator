import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { ReferenceImageSelection, Template } from '@/types'
import { TemplateCreateDialog } from './TemplateCreateDialog'

const mocks = vi.hoisted(() => ({
  cancel: vi.fn(),
  create: vi.fn(),
  get: vi.fn(),
  onOpenChange: vi.fn(),
  retry: vi.fn(),
  success: vi.fn(),
  update: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  api: {
    templates: {
      create: mocks.create,
      get: mocks.get,
      update: mocks.update,
    },
    imageAnalyses: {
      cancel: mocks.cancel,
      retry: mocks.retry,
    },
  },
}))

vi.mock('@/components/projects/ReferenceAssetUpload', () => ({
  ReferenceAssetUpload: ({
    disabled,
    onChange,
  }: {
    disabled?: boolean
    onChange: (value: ReferenceImageSelection) => void
  }) => (
    <button
      type="button"
      disabled={disabled}
      onClick={() => onChange({ upload_session_id: 'thumbnail-upload' })}
    >
      上传缩略图
    </button>
  ),
}))

vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: mocks.success } }))

function renderDialog(template?: Template, cachedTemplate?: Template) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  if (template && cachedTemplate) client.setQueryData(['template', template.id], cachedTemplate)
  return render(
    <QueryClientProvider client={client}>
      <TemplateCreateDialog open onOpenChange={mocks.onOpenChange} template={template} />
    </QueryClientProvider>,
  )
}

const analyzedTemplate: Template = {
  id: 'template-analyzed',
  type: 'seednote',
  name: '自动识别模板',
  category: '好物种草',
  thumbnail: {
    asset_id: 'thumbnail-old',
    file_name: 'old.png',
    content_type: 'image/png',
    size: 100,
    download_url: 'https://cdn.example/old.png',
    download_expires_at: '2026-09-18T11:00:00Z',
  },
  prompt: '旧的自动识别 Prompt',
  prompt_source: 'analysis',
  readiness_status: 'ready',
  activate_when_ready: true,
  visibility: 'public',
  sort_order: 0,
  is_active: true,
  created_at: '2026-09-18T09:00:00Z',
  updated_at: '2026-09-18T10:00:00Z',
}

describe('TemplateCreateDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.create.mockResolvedValue({ id: 'template-1' })
  })

  it('空 Prompt 创建时提交缩略图上传会话并立即进入后台识别', async () => {
    renderDialog()

    fireEvent.change(screen.getByLabelText('模板名称'), { target: { value: '清透说明书' } })
    fireEvent.click(screen.getByRole('button', { name: '上传缩略图' }))
    fireEvent.click(screen.getByRole('button', { name: '保存模板' }))

    await waitFor(() => expect(mocks.create).toHaveBeenCalledWith({
      name: '清透说明书',
      type: 'seednote',
      category: '好物种草',
      thumbnail_image: { upload_session_id: 'thumbnail-upload' },
      prompt: undefined,
      visibility: 'public',
      sort_order: 0,
      is_active: true,
    }))
    expect(mocks.onOpenChange).toHaveBeenCalledWith(false)
    expect(mocks.success).toHaveBeenCalledWith('模板已创建，正在后台识别')
    expect(mocks.retry).not.toHaveBeenCalled()
    expect(mocks.cancel).not.toHaveBeenCalled()
  })

  it('手动 Prompt 随创建请求提交且不触发图片识别动作', async () => {
    renderDialog()

    fireEvent.change(screen.getByLabelText('模板名称'), { target: { value: '品牌手册' } })
    fireEvent.click(screen.getByRole('button', { name: '上传缩略图' }))
    fireEvent.change(screen.getByLabelText('缩略图与视觉版式 Prompt'), {
      target: { value: '  人工填写的视觉与版式 Prompt  ' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存模板' }))

    await waitFor(() => expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({
      thumbnail_image: { upload_session_id: 'thumbnail-upload' },
      prompt: '人工填写的视觉与版式 Prompt',
    })))
    expect(mocks.onOpenChange).toHaveBeenCalledWith(false)
    expect(mocks.success).toHaveBeenCalledWith('模板已创建')
    expect(mocks.retry).not.toHaveBeenCalled()
    expect(mocks.cancel).not.toHaveBeenCalled()
  })

  it('编辑自动识别模板时换图不把旧 Prompt 提交为人工内容', async () => {
    mocks.update.mockResolvedValue({ ...analyzedTemplate })
    renderDialog(analyzedTemplate)

    fireEvent.click(screen.getByRole('button', { name: '上传缩略图' }))
    fireEvent.click(screen.getByRole('button', { name: '保存模板' }))

    await waitFor(() => expect(mocks.update).toHaveBeenCalledWith(
      analyzedTemplate.id,
      {
        thumbnail_image: { upload_session_id: 'thumbnail-upload' },
      },
    ))
  })

  it('编辑模板只提交实际修改的字段', async () => {
    mocks.update.mockResolvedValue({ ...analyzedTemplate, name: '新名称' })
    renderDialog(analyzedTemplate)

    fireEvent.change(screen.getByLabelText('模板名称'), { target: { value: '新名称' } })
    fireEvent.click(screen.getByRole('button', { name: '保存模板' }))

    await waitFor(() => expect(mocks.update).toHaveBeenCalledWith(analyzedTemplate.id, { name: '新名称' }))
  })

  it('详情缓存仍新鲜时也会立即刷新活动识别', async () => {
    const running: Template = {
      ...analyzedTemplate,
      prompt: '',
      readiness_status: 'analyzing',
      image_analysis: {
        id: 'analysis-running-cache',
        kind: 'template_prompt',
        status: 'queued',
        attempt_count: 0,
        can_retry: false,
        updated_at: '2026-09-18T10:02:00Z',
      },
    }
    mocks.get.mockResolvedValue(running)

    renderDialog(running, analyzedTemplate)

    await waitFor(() => expect(mocks.get).toHaveBeenCalledWith(running.id, expect.any(AbortSignal)))
  })

  it('打开的识别中模板会轮询详情并在完成后解锁', async () => {
    let resolveGet!: (value: Template) => void
    mocks.get.mockReturnValue(new Promise<Template>((resolve) => { resolveGet = resolve }))
    const running: Template = {
      ...analyzedTemplate,
      prompt: '',
      prompt_source: '',
      readiness_status: 'analyzing',
      is_active: false,
      image_analysis: {
        id: 'analysis-running',
        kind: 'template_prompt',
        status: 'running',
        attempt_count: 1,
        can_retry: false,
        updated_at: '2026-09-18T10:00:00Z',
      },
    }
    renderDialog(running)

    expect(screen.getByLabelText('缩略图与视觉版式 Prompt')).toBeDisabled()
    resolveGet({
      ...analyzedTemplate,
      prompt: '最新自动识别 Prompt',
      image_analysis: {
        ...running.image_analysis!,
        status: 'succeeded',
        updated_at: '2026-09-18T10:01:00Z',
      },
    })

    await waitFor(() => expect(screen.getByLabelText('缩略图与视觉版式 Prompt')).toBeEnabled())
    expect(screen.getByLabelText('缩略图与视觉版式 Prompt')).toHaveValue('最新自动识别 Prompt')
  })
})
