import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ViralAnalysisTab from './ViralAnalysisTab'
import { api } from '@/lib/api'
import type { ViralAnalysis } from '@/types'

vi.mock('@/lib/api', () => ({
  api: {
    viralAnalyses: {
      list: vi.fn(),
      get: vi.fn(),
      create: vi.fn(),
    },
  },
}))

function analysis(id: string, status: ViralAnalysis['status'] = 'analyzing'): ViralAnalysis {
  return {
    id,
    user_id: 'user-1',
    source_type: 'note',
    source_url: `https://www.xiaohongshu.com/explore/${id}`,
    source_data: {},
    analysis_result: null,
    status,
    created_at: '2025-01-15T10:00:00Z',
    updated_at: '2025-01-15T10:00:00Z',
  }
}

function SwitchAnalysisButton() {
  const navigate = useNavigate()
  return (
    <button type="button" onClick={() => navigate('/workshop?tab=analysis&analysisId=analysis-2')}>
      switch analysis
    </button>
  )
}

function LocationProbe() {
  const location = useLocation()
  return <span data-testid="location">{location.pathname}{location.search}</span>
}

function renderTab(initialEntry = '/workshop?tab=analysis&analysisId=analysis-1') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route
            path="/workshop"
            element={
              <>
                <SwitchAnalysisButton />
                <LocationProbe />
                <ViralAnalysisTab />
              </>
            }
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

async function flushPromises() {
  await act(async () => {
    await Promise.resolve()
  })
}

describe('ViralAnalysisTab polling', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.mocked(api.viralAnalyses.list).mockResolvedValue({
      items: [analysis('analysis-1'), analysis('analysis-2')],
      total: 2,
    })
    vi.mocked(api.viralAnalyses.get).mockImplementation(async (id) => analysis(id, 'analyzing'))
    vi.mocked(api.viralAnalyses.create).mockResolvedValue(analysis('analysis-created', 'pending'))
  })

  afterEach(() => {
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.resetAllMocks()
  })

  it('switches polling to the new analysisId without keeping the old loop', async () => {
    const { unmount } = renderTab()

    await flushPromises()
    expect(api.viralAnalyses.list).toHaveBeenCalled()
    await act(async () => {
      vi.advanceTimersByTime(3000)
    })
    await flushPromises()
    expect(api.viralAnalyses.get).toHaveBeenCalledWith('analysis-1')

    fireEvent.click(screen.getByRole('button', { name: 'switch analysis' }))
    await flushPromises()
    await act(async () => {
      vi.advanceTimersByTime(3000)
    })
    await flushPromises()

    expect(api.viralAnalyses.get).toHaveBeenCalledWith('analysis-2')
    const requestedIDs = vi.mocked(api.viralAnalyses.get).mock.calls.map(([id]) => id)
    expect(requestedIDs.slice(-1)).toEqual(['analysis-2'])

    unmount()
  })

  it('starts polling when selecting a running analysis from history', async () => {
    vi.useRealTimers()
    renderTab('/workshop?tab=analysis')

    fireEvent.click(await screen.findByText('analysis-2'))
    vi.useFakeTimers()
    await flushPromises()
    await act(async () => {
      vi.advanceTimersByTime(3000)
    })
    await flushPromises()

    expect(api.viralAnalyses.get).toHaveBeenCalledWith('analysis-2')
    expect(screen.getByTestId('location')).toHaveTextContent('analysisId=analysis-2')
  })
})
