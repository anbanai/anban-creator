import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { describe, it, expect, vi } from 'vitest'
import { api } from '@/lib/api'
import { TopicDiscovery, discoveryNeedsPolling } from './TopicPanels'
import type { TopicDigestDetail } from '@/types/topic-radar'
const detail: TopicDigestDetail = { digest: { id: 'run', subscription_id: 'sub', status: 'succeeded', dispatch_status: '', candidate_count: 0, planned_at: '', created_at: '' }, scripts: [], suggestions: [], adoptions: [], notifications: [] }
describe('discovery side effects', () => {
  it.each(['pending', 'processing'])('polls adoption %s', status => {
    expect(discoveryNeedsPolling({ ...detail, adoptions: [{ id: 'a', suggestion_id: 's', status }] })).toBe(true)
  })
  it('polls retrying notifications but stops terminal delivery', () => {
    expect(discoveryNeedsPolling({ ...detail, notifications: [{ id: 'n', channel_id: 'c', status: 'failed', attempt: 1, next_attempt_at: '2026-11-04' }] })).toBe(true)
    expect(discoveryNeedsPolling(detail)).toBe(false)
  })
  it('updates completed scripts after research has already succeeded', async () => {
    const script = { id: 'script', suggestion_id: 's', digest_id: 'run', aspect_ratio: '9:16', target_platform: 'douyin', duration_seconds: 60, status: 'running' }
    const get = vi.spyOn(api.topicRadar, 'digest').mockResolvedValueOnce({ ...detail, scripts: [script] }).mockResolvedValue({ ...detail, scripts: [{ ...script, status: 'succeeded' }] })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    render(<QueryClientProvider client={client}><MemoryRouter><TopicDiscovery projectId="" settings={false} params={new URLSearchParams('run=run')} update={vi.fn()} /></MemoryRouter></QueryClientProvider>)
    fireEvent.click(await screen.findByText('本次证据与执行记录'))
    expect(await screen.findByText(/douyin.*生成中/)).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: '查看脚本' }, { timeout: 4500 })).toBeInTheDocument()
    expect(get).toHaveBeenCalledTimes(2)
    for (const key of ['content-assets', 'projects', 'project-stats']) {
      expect(invalidate.mock.calls.filter(([filter]) => filter?.queryKey?.[0] === key)).toHaveLength(2)
    }
  })
})
