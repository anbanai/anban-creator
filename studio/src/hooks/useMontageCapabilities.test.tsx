import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useMontageCapabilities } from './useMontageCapabilities'

vi.mock('@/lib/api', () => ({
  api: {
    montageCapabilities: {
      list: vi.fn(),
    },
  },
}))

describe('useMontageCapabilities', () => {
  beforeEach(() => {
    vi.mocked(api.montageCapabilities.list).mockReset()
  })

  it('exposes catalog metadata and pipeline lookup', async () => {
    vi.mocked(api.montageCapabilities.list).mockResolvedValueOnce({
      enabled: true,
      default_pipeline: 'cinematic',
      max_duration_seconds: 600,
      max_assets: 20,
      items: [{
        key: 'cinematic',
        display_name: '电影感制作',
        description: '品牌片、预告片与情绪叙事',
        best_for: ['品牌发布'],
        source_hint: '可使用视频、图片，也可仅根据创意说明生成',
        output_hint: '一条完整成片',
        source_requirement: 'optional',
        output_mode: 'single',
        recommended_duration_seconds: 30,
      }],
    })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    )

    const { result } = renderHook(() => useMontageCapabilities(), { wrapper })

    await waitFor(() => expect(result.current.isLoading).toBe(false))
    expect(result.current.enabled).toBe(true)
    expect(result.current.defaultPipeline).toBe('cinematic')
    expect(result.current.maxDurationSeconds).toBe(600)
    expect(result.current.maxAssets).toBe(20)
    expect(result.current.capabilityByKey('cinematic')?.display_name).toBe('电影感制作')
    expect(result.current.capabilityByKey('retired')).toBeUndefined()
  })
})
