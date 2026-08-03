import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAgentPacks } from './useAgentPacks'

vi.mock('@/lib/api', () => ({
  api: {
    agentPacks: {
      list: vi.fn(),
    },
  },
}))

describe('useAgentPacks', () => {
  beforeEach(() => {
    vi.mocked(api.agentPacks.list).mockReset()
  })

  it('normalizes a partial catalog response to an empty pack list', async () => {
    vi.mocked(api.agentPacks.list).mockResolvedValueOnce({} as Awaited<ReturnType<typeof api.agentPacks.list>>)
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    )

    const { result } = renderHook(() => useAgentPacks(), { wrapper })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data).toEqual({ packs: [] })
  })
})
