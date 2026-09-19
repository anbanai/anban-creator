import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { ImageAnalysis } from '@/types'
import { AnalyzedImageField } from './AnalyzedImageField'

const apiMocks = vi.hoisted(() => ({
  cancel: vi.fn(),
  retry: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  api: { imageAnalyses: apiMocks },
}))

vi.mock('@/components/projects/ReferenceAssetUpload', () => ({
  ReferenceAssetUpload: ({ disabled }: { disabled?: boolean }) => (
    <button type="button" disabled={disabled}>上传图片</button>
  ),
}))

vi.mock('sonner', () => ({ toast: { error: vi.fn() } }))

const runningAnalysis: ImageAnalysis = {
  id: 'analysis-running',
  kind: 'template_prompt',
  status: 'running',
  attempt_count: 1,
  can_retry: false,
  updated_at: '2026-09-18T10:00:00Z',
}

const failedAnalysis: ImageAnalysis = {
  id: 'analysis-failed',
  kind: 'template_prompt',
  status: 'failed',
  attempt_count: 3,
  error_code: 'provider_unavailable',
  error_message: '图片服务暂时不可用，请稍后重试',
  can_retry: true,
  updated_at: '2026-09-18T10:05:00Z',
}

function renderField(analysis: ImageAnalysis, onAnalysisAction = vi.fn(), onBeforeAnalysisAction = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <AnalyzedImageField
        asset={null}
        onAssetChange={() => {}}
        purpose="template_thumbnail"
        text=""
        onTextChange={() => {}}
        analysis={analysis}
        placeholder="视觉 Prompt"
        textId="analysis-prompt"
        onBeforeAnalysisAction={onBeforeAnalysisAction}
        onAnalysisAction={onAnalysisAction}
      />
    </QueryClientProvider>,
  )
  return { onAnalysisAction, onBeforeAnalysisAction }
}

describe('AnalyzedImageField', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.cancel.mockResolvedValue({ cancelled: true })
    apiMocks.retry.mockResolvedValue({ ...failedAnalysis, status: 'queued' })
  })

  it('识别运行中锁定图片与文本并允许显式停止后刷新', async () => {
    const { onAnalysisAction, onBeforeAnalysisAction } = renderField(runningAnalysis)

    expect(screen.getByRole('button', { name: '上传图片' })).toBeDisabled()
    expect(screen.getByRole('textbox')).toBeDisabled()
    expect(screen.getByText('正在后台识别图片')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '停止识别并手动填写' }))

    await waitFor(() => expect(apiMocks.cancel).toHaveBeenCalledWith('analysis-running'))
    expect(onBeforeAnalysisAction).toHaveBeenCalledOnce()
    expect(onBeforeAnalysisAction.mock.invocationCallOrder[0]).toBeLessThan(apiMocks.cancel.mock.invocationCallOrder[0])
    expect(onAnalysisAction).toHaveBeenCalledOnce()
  })

  it('识别失败后展示安全错误并允许重试', async () => {
    const { onAnalysisAction, onBeforeAnalysisAction } = renderField(failedAnalysis)

    expect(screen.getByText('图片服务暂时不可用，请稍后重试')).toBeInTheDocument()
    expect(screen.getByRole('textbox')).toBeEnabled()
    expect(screen.getByRole('button', { name: '上传图片' })).toBeEnabled()

    fireEvent.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => expect(apiMocks.retry).toHaveBeenCalledWith('analysis-failed'))
    expect(onBeforeAnalysisAction).toHaveBeenCalledOnce()
    expect(onBeforeAnalysisAction.mock.invocationCallOrder[0]).toBeLessThan(apiMocks.retry.mock.invocationCallOrder[0])
    expect(onAnalysisAction).toHaveBeenCalledOnce()
  })
})
