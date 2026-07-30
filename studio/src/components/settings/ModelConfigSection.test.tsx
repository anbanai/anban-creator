import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'

import ModelConfigSection from './ModelConfigSection'
import { api } from '@/lib/api'

vi.mock('@/lib/api', () => ({
  api: { modelConfig: { get: vi.fn(), update: vi.fn(), clear: vi.fn() } },
}))

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.modelConfig.get).mockResolvedValue({
    image: { provider: 'openai', endpoint: 'https://images.example.com/v1', api_key: '****', model: 'gpt-image-1' },
  })
  vi.mocked(api.modelConfig.update).mockResolvedValue({ code: 0, msg: 'ok', data: null })
})

it('only renders and saves the image model override', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <ModelConfigSection />
    </QueryClientProvider>,
  )

  expect(await screen.findByText('图片生成模型覆盖')).toBeInTheDocument()
  expect(screen.queryByText('文本模型')).not.toBeInTheDocument()
  expect(screen.queryByText('写作模型')).not.toBeInTheDocument()
  expect(screen.queryByText('MCP 模型配置')).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '图片生成模型覆盖' }))
  fireEvent.click(screen.getByRole('button', { name: '保存图片模型配置' }))
  await waitFor(() => expect(api.modelConfig.update).toHaveBeenCalledWith({
    image: { provider: 'openai', endpoint: 'https://images.example.com/v1', api_key: '****', model: 'gpt-image-1', proxy: '' },
  }))
})
