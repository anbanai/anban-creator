import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import TimelinePage from './TimelinePage'
import { api } from '@/lib/api'

const toastError = vi.hoisted(() => vi.fn())
vi.mock('sonner', () => ({ toast: { error: toastError, success: vi.fn() } }))

describe('TimelinePage filters', () => {
  it('uses the same page heading pattern as other workspace pages', () => {
    render(<TimelinePage />)
    expect(screen.getByRole('heading', { level: 1, name: '时间线' })).toBeInTheDocument()
  })

  it('uses a shared status pill and 44px icon action for schedule controls', async () => {
    vi.spyOn(api.timeline, 'get').mockResolvedValueOnce({
      items: [{
        id: 'active-plan', type: 'plan', content_type: 'seednote', title: '每周选题', status: 'active',
        project_id: 'project-1', project_name: '种草账号', agent_ids: ['seednote'],
        scheduled_at: '2026-10-09T09:00:00Z', created_at: '2026-10-08T09:00:00Z', completed_at: '',
      }],
    })
    render(<TimelinePage />)

    await screen.findByText('每周选题')
    expect(screen.getByTestId('status-pill')).toHaveAttribute('data-status', 'active')
    expect(screen.getByRole('button', { name: '暂停计划：每周选题' })).toHaveClass('size-11')
    expect(screen.queryByRole('button', { name: '暂停' })).not.toBeInTheDocument()
  })

  it('disables a schedule action while pending and reports a failed update', async () => {
    vi.spyOn(api.timeline, 'get').mockResolvedValueOnce({
      items: [{
        id: 'active-plan', type: 'plan', content_type: 'seednote', title: '每周选题', status: 'active',
        project_id: 'project-1', project_name: '种草账号', agent_ids: ['seednote'],
        scheduled_at: '2026-10-09T09:00:00Z', created_at: '2026-10-08T09:00:00Z', completed_at: '',
      }],
    })
    let rejectPause!: (error: Error) => void
    vi.spyOn(api.plans, 'pause').mockReturnValueOnce(new Promise((_, reject) => { rejectPause = reject }))
    render(<TimelinePage />)

    const pause = await screen.findByRole('button', { name: '暂停计划：每周选题' })
    fireEvent.click(pause)
    await waitFor(() => expect(pause).toBeDisabled())
    rejectPause(new Error('pause failed'))
    await waitFor(() => expect(toastError).toHaveBeenCalledWith('pause failed'))
  })

  it('renders every derived project Agent icon in a timeline item', async () => {
    const timeline = vi.spyOn(api.timeline, 'get').mockResolvedValueOnce({
      items: [{
        id: 'multi-agent-task',
        type: 'task',
        content_type: 'montage',
        title: '多 Agent 任务',
        status: 'pending',
        project_id: 'multi-agent-project',
        project_name: '多 Agent 项目',
        platform: 'wechat',
        agent_ids: ['montage', 'seednote', 'hypit'],
        scheduled_at: '2026-09-24T09:00:00Z',
        created_at: '2026-09-24T09:00:00Z',
        completed_at: '',
      }],
    })
    render(<TimelinePage />)
    await screen.findByText('多 Agent 任务')
    expect(document.querySelector('[data-agent-id="montage"]')).toBeInTheDocument()
    expect(document.querySelector('[data-agent-id="seednote"]')).toBeInTheDocument()
    expect(document.querySelector('[data-agent-id="hypit"]')).toBeInTheDocument()
    timeline.mockRestore()
  })

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
    const montage = await screen.findByText('视频生成', { selector: '[data-slot="badge"]' })
    const hypit = screen.getByText('视频复刻', { selector: '[data-slot="badge"]' })
    expect(montage).toHaveClass('text-purple-700')
    expect(montage.querySelector('[data-platform-icon="montage"]')).toBeInTheDocument()
    expect(montage.querySelector('[data-platform-symbol="generate"]')).toBeInTheDocument()
    expect(hypit).toHaveClass('text-orange-700')
    expect(hypit.querySelector('[data-platform-icon="hypit"]')).toBeInTheDocument()
    expect(hypit.querySelector('[data-platform-symbol="replicate"]')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('combobox', { name: '内容类型' }))
    expect(await screen.findByRole('option', { name: '视频生成' })).toBeInTheDocument()
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
