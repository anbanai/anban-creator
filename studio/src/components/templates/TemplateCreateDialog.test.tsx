import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { TemplateCreateDialog } from './TemplateCreateDialog'

const apiMocks = vi.hoisted(() => ({
  analyzeThumbnail: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  api: {
    templates: apiMocks,
  },
}))

vi.mock('@/components/projects/ReferenceImageUpload', () => ({
  ReferenceImageUpload: ({ onChange }: { onChange?: (url: string) => void }) => (
    <button type="button" onClick={() => onChange?.('https://cdn.example/template.png')}>上传缩略图</button>
  ),
}))

vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((next) => { resolve = next })
  return { promise, resolve }
}

function renderDialog() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <TemplateCreateDialog open onOpenChange={() => {}} />
    </QueryClientProvider>,
  )
}

describe('TemplateCreateDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.create.mockResolvedValue({ id: 'template-1' })
  })

  it('缩略图分析异步返回时不覆盖用户已编辑的 Prompt', async () => {
    const analysis = deferred<{ prompt: string }>()
    apiMocks.analyzeThumbnail.mockReturnValue(analysis.promise)
    renderDialog()

    fireEvent.click(screen.getByRole('button', { name: '上传缩略图' }))
    const prompt = screen.getByLabelText('视觉与版式 Prompt')
    fireEvent.change(prompt, { target: { value: '人工调整后的 Prompt' } })
    analysis.resolve({ prompt: '分析生成的 Prompt' })

    await waitFor(() => expect(apiMocks.analyzeThumbnail).toHaveBeenCalledWith({
      type: 'seednote',
      thumbnail_url: 'https://cdn.example/template.png',
    }))
    expect(prompt).toHaveValue('人工调整后的 Prompt')
  })

  it('未手动编辑时自动填入分析结果并提交固定行业、排序和启停字段', async () => {
    apiMocks.analyzeThumbnail.mockResolvedValue({ prompt: '分析生成的 Prompt' })
    renderDialog()

    fireEvent.change(screen.getByLabelText('模板名称'), { target: { value: '清透说明书' } })
    fireEvent.click(screen.getByRole('button', { name: '上传缩略图' }))
    expect(await screen.findByDisplayValue('分析生成的 Prompt')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('combobox', { name: '行业分类' }))
    fireEvent.click(await screen.findByRole('option', { name: '美妆护肤' }))
    fireEvent.change(screen.getByLabelText('排序'), { target: { value: '30' } })
    fireEvent.click(screen.getByRole('button', { name: '保存模板' }))

    await waitFor(() => expect(apiMocks.create).toHaveBeenCalledWith({
      name: '清透说明书',
      type: 'seednote',
      category: '美妆护肤',
      thumbnail_url: 'https://cdn.example/template.png',
      prompt: '分析生成的 Prompt',
      visibility: 'public',
      sort_order: 30,
      is_active: true,
    }))
  })

  it('人工编辑后点击重新分析会应用本次分析结果', async () => {
    apiMocks.analyzeThumbnail
      .mockResolvedValueOnce({ prompt: '首次自动分析 Prompt' })
      .mockResolvedValueOnce({ prompt: '显式重新分析 Prompt' })
    renderDialog()

    fireEvent.click(screen.getByRole('button', { name: '上传缩略图' }))
    const prompt = await screen.findByDisplayValue('首次自动分析 Prompt')
    fireEvent.change(prompt, { target: { value: '人工调整后的 Prompt' } })
    fireEvent.click(screen.getByRole('button', { name: '重新分析' }))

    expect(await screen.findByDisplayValue('显式重新分析 Prompt')).toBeInTheDocument()
    expect(apiMocks.analyzeThumbnail).toHaveBeenCalledTimes(2)
  })
})
