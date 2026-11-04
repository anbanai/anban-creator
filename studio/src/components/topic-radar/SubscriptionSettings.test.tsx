import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import { SubscriptionSettings } from './SubscriptionSettings'

const { updateSubscription, deleteSubscription } = vi.hoisted(() => ({ updateSubscription: vi.fn(), deleteSubscription: vi.fn() }))
vi.mock('@/lib/api', () => ({ api: { topicRadar: { updateSubscription, deleteSubscription } } }))

describe('SubscriptionSettings', () => {
  it('keeps script format controls hidden until the user asks to adjust them', () => {
    render(<SubscriptionSettings subscription={{ id: 'sub-1', project_id: 'project-1', name: '每日选题', enabled: false, timezone: 'Asia/Shanghai', source_config: {}, script_policy: { enabled: false } }} onClose={vi.fn()} onSaved={vi.fn().mockResolvedValue(undefined)} />)

    expect(screen.getByRole('heading', { name: '自动找选题' })).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: '每天自动找选题' })).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: '自动生成短视频脚本' })).toBeInTheDocument()
    expect(screen.queryByLabelText('每天几点运行')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('计划名称')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '删除订阅' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('switch', { name: '自动生成短视频脚本' }))
    expect(screen.getByLabelText('每次生成数量')).not.toBeVisible()
    expect(screen.getByText('调整脚本格式').closest('details')).not.toHaveAttribute('open')
    fireEvent.click(screen.getByText('调整脚本格式'))
    expect(screen.getByLabelText('每次生成数量')).toBeInTheDocument()

    fireEvent.click(screen.getByText('更多设置'))
    expect(screen.queryByLabelText('设置名称')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('时区')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('流量潜力')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '删除自动找题设置' })).toBeInTheDocument()
    expect(screen.getByLabelText('每次生成数量')).toBeVisible()

  })

  it('reveals the daily time only after automatic topic discovery is enabled', () => {
    render(<SubscriptionSettings subscription={{ id: 'sub-1', project_id: 'project-1', name: '每日选题', enabled: false, timezone: 'Asia/Shanghai', source_config: {} }} onClose={vi.fn()} onSaved={vi.fn().mockResolvedValue(undefined)} />)

    fireEvent.click(screen.getByRole('switch', { name: '每天自动找选题' }))

    expect(screen.getByLabelText('每天几点运行')).toHaveValue('09:00')
  })

  it('deletes the subscription only after confirmation', async () => {
    deleteSubscription.mockResolvedValue({ deleted: true })
    const onDeleted = vi.fn().mockResolvedValue(undefined)
    render(<SubscriptionSettings subscription={{ id: 'sub-1', project_id: 'project-1', name: '每日选题', enabled: true, timezone: 'Asia/Shanghai', source_config: {} }} onClose={vi.fn()} onSaved={vi.fn().mockResolvedValue(undefined)} onDeleted={onDeleted} />)
    fireEvent.click(screen.getByText('更多设置'))
    fireEvent.click(screen.getByRole('button', { name: '删除自动找题设置' }))
    expect(deleteSubscription).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '确认删除' }))
    await waitFor(() => expect(deleteSubscription).toHaveBeenCalledWith('sub-1'))
    expect(onDeleted).toHaveBeenCalledOnce()
  })

  it('opens and saves a valid empty source configuration using all sources', async () => {
    updateSubscription.mockResolvedValue({})
    const onSaved = vi.fn().mockResolvedValue(undefined)
    render(<SubscriptionSettings subscription={{ id: 'sub-1', project_id: 'project-1', name: '每日选题', enabled: true, timezone: 'Asia/Shanghai', source_config: {} }} onClose={vi.fn()} onSaved={onSaved} />)
    expect(screen.getByRole('switch', { name: '自动生成短视频脚本' })).toBeInTheDocument()
    expect(screen.getByLabelText('每天几点运行')).toHaveValue('09:00')
    const advanced = screen.getByText('更多设置').closest('details')!
    expect(advanced).not.toHaveAttribute('open')
    fireEvent.click(screen.getByText('更多设置'))
    expect(screen.getByText('未选择时使用全部支持的趋势来源。')).toBeInTheDocument()
    expect(screen.getByLabelText('微博')).not.toBeChecked()
    fireEvent.click(screen.getByRole('button', { name: '保存配置' }))
    await waitFor(() => expect(updateSubscription).toHaveBeenCalledWith('sub-1', expect.objectContaining({ source_config: { platforms: [] }, schedule: { hour: 9, minute: 0 } })))
    expect(onSaved).toHaveBeenCalledOnce()
  })

  it('preserves existing advanced values when saving simple settings', async () => {
    updateSubscription.mockResolvedValue({})
    const weights = { traffic_potential: .4, account_fit: .1, differentiation: .1, timeliness: .1, monetization: .1, production_cost: .1, compliance_risk: .1 }
    render(<SubscriptionSettings subscription={{ id: 'sub-1', project_id: 'project-1', name: '自定义名称', enabled: true, timezone: 'UTC', schedule: { hour: 11, minute: 30 }, source_config: { platforms: ['weibo'] }, weights }} onClose={vi.fn()} onSaved={vi.fn().mockResolvedValue(undefined)} />)
    fireEvent.click(screen.getByRole('button', { name: '保存配置' }))

    await waitFor(() => expect(updateSubscription).toHaveBeenCalledWith('sub-1', expect.objectContaining({ name: '自定义名称', timezone: 'UTC', weights })))
  })

  it('saves default script settings when automatic script generation is enabled', async () => {
    updateSubscription.mockResolvedValue({})
    render(<SubscriptionSettings subscription={{ id: 'sub-1', project_id: 'project-1', name: '每日选题', enabled: false, timezone: 'Asia/Shanghai', source_config: {}, script_policy: { enabled: false } }} onClose={vi.fn()} onSaved={vi.fn().mockResolvedValue(undefined)} />)
    fireEvent.click(screen.getByRole('switch', { name: '自动生成短视频脚本' }))
    fireEvent.click(screen.getByRole('button', { name: '保存配置' }))

    await waitFor(() => expect(updateSubscription).toHaveBeenCalledWith('sub-1', expect.objectContaining({
      script_policy: expect.objectContaining({ enabled: true, top_n: 1, platform: 'douyin', duration_seconds: 60, aspect_ratio: '9:16' }),
    })))
  })
})
