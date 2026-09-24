import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import TimelinePage from './TimelinePage'
import { api } from '@/lib/api'

describe('TimelinePage filters', () => {
  it('distinguishes video generation and replication in cards and filters', async () => {
    const timeline = vi.spyOn(api.timeline, 'get').mockResolvedValueOnce({
      items: (['montage', 'hypit'] as const).map(content_type => ({
        id: content_type,
        type: 'task' as const,
        content_type,
        title: `${content_type} task`,
        status: 'pending' as const,
        scheduled_at: '2026-09-24T09:00:00Z',
        created_at: '2026-09-24T09:00:00Z',
        completed_at: '',
      })),
    })
    render(<TimelinePage />)
    const montage = await screen.findByText('Montage 视频生成', { selector: '[data-slot="badge"]' })
    const hypit = screen.getByText('视频复刻', { selector: '[data-slot="badge"]' })
    expect(montage).toHaveClass('text-purple-700')
    expect(montage.querySelector('svg')).toHaveClass('lucide-clapperboard')
    expect(hypit).toHaveClass('text-orange-700')
    expect(hypit.querySelector('svg')).toHaveClass('lucide-repeat-2')
    fireEvent.click(screen.getByRole('combobox', { name: '内容类型' }))
    expect(await screen.findByRole('option', { name: 'Montage 视频生成' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: '视频复刻' })).toBeInTheDocument()
    timeline.mockRestore()
  })

  it('shows meaningful labels before any filter is opened', () => {
    render(<TimelinePage />)
    for (const [name, value] of [['条目类型', '全部类型'], ['内容类型', '全部内容'], ['状态', '全部状态'], ['排序方式', '日期 ↓']]) {
      expect(screen.getByRole('combobox', { name })).toHaveTextContent(value)
    }
  })
})
