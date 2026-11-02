import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { render } from '@/test/test-utils'
import TopicRadarPage from './TopicRadarPage'

const { subscriptions, digests, suggestions, run, createSubscription, requestScript, digest, updateSubscription, deleteSubscription, retryScript, scriptArtifact } = vi.hoisted(() => ({
  subscriptions: vi.fn(), digests: vi.fn(), suggestions: vi.fn(), run: vi.fn(), createSubscription: vi.fn(), requestScript: vi.fn(), digest: vi.fn(), updateSubscription: vi.fn(), deleteSubscription: vi.fn(), retryScript: vi.fn(), scriptArtifact: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  api: {
    projects: { list: vi.fn(async () => [{ id: 'project-1', name: '生活观察' }]) },
    topicRadar: { subscriptions, digests, suggestions, run, createSubscription, requestScript, digest, updateSubscription, deleteSubscription, retryScript, scriptArtifact },
  },
}))
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

beforeEach(() => {
  vi.clearAllMocks()
  subscriptions.mockResolvedValue({ items: [{ id: 'sub-1', project_id: 'project-1', name: '选题雷达', enabled: false, timezone: 'Asia/Shanghai' }] })
  digests.mockResolvedValue({ items: [{ id: 'digest-1', subscription_id: 'sub-1', status: 'succeeded', dispatch_status: 'claimed', candidate_count: 1, planned_at: '2026-10-30T08:00:00Z', created_at: '2026-10-30T08:00:00Z' }] })
  suggestions.mockResolvedValue({ items: [{
    id: 'suggestion-1', digest_id: 'digest-1', title: '城市里的微型菜园', introduction: '从阳台种植切入，讲清低成本入门方法。',
    source_summary: '微博近期上升趋势', source_refs: [{ type: 'trend', platform: '微博', title: '阳台种菜', url: 'https://example.test/trend', rank: 3, freshness: 'fresh' }],
    scores: { traffic_potential: 0.82, account_fit: 0.7 }, video_fit: '适合 45 秒教程', rank: 1, adoption_status: 'adopted',
  }] })
  digest.mockImplementation(async () => ({digest: (await digests()).items[0], suggestions: (await suggestions()).items, adoptions: [], scripts: [], notifications: []}))
  updateSubscription.mockResolvedValue({id:'sub-1'})
  retryScript.mockResolvedValue({id:'script-1',status:'queued'})
  run.mockResolvedValue({ id: 'digest-2', status: 'queued' })
  requestScript.mockResolvedValue({ id: 'job-1', status: 'queued' })
})

describe('TopicRadarPage', () => {
  it('saves subscription schedule and automatically generates selected scripts', async () => {
    render(<TopicRadarPage />)
    await screen.findByText('城市里的微型菜园')
    fireEvent.click(screen.getByRole('button', {name:'订阅配置'}))
    fireEvent.click(screen.getByLabelText('开启定时分析'))
    fireEvent.click(screen.getByLabelText('自动生成短视频脚本'))
    fireEvent.click(screen.getByRole('button', {name:'保存配置'}))
    await waitFor(() => expect(updateSubscription).toHaveBeenCalledWith('sub-1',expect.objectContaining({enabled:true,script_policy:expect.objectContaining({enabled:true})})))
  })
  it('clears the deleted subscription selection after confirmation', async () => {
    subscriptions.mockReset()
    subscriptions.mockResolvedValueOnce({ items: [{ id: 'sub-1', project_id: 'project-1', name: '选题雷达', enabled: false, timezone: 'Asia/Shanghai' }] })
    subscriptions.mockResolvedValue({ items: [] })
    deleteSubscription.mockResolvedValue({ deleted: true })
    render(<TopicRadarPage />)
    await screen.findByText('城市里的微型菜园')
    fireEvent.click(screen.getByRole('button', { name: '订阅配置' }))
    fireEvent.click(screen.getByRole('button', { name: '删除订阅' }))
    fireEvent.click(screen.getByRole('button', { name: '确认删除' }))
    await waitFor(() => expect(deleteSubscription).toHaveBeenCalledWith('sub-1'))
    await waitFor(() => expect(screen.getByRole('button', { name: '创建选题雷达' })).toBeInTheDocument())
  })

  it('shows failed scripts separately and retries the durable job', async () => {
    digest.mockImplementation(async () => ({digest:(await digests()).items[0],suggestions:(await suggestions()).items,adoptions:[],notifications:[],scripts:[{id:'script-1',suggestion_id:'suggestion-1',status:'failed',target_platform:'douyin',duration_seconds:60,error:'生成失败'}]}))
    render(<TopicRadarPage />)
    expect(await screen.findByText('生成失败')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button',{name:'重试脚本'}))
    await waitFor(() => expect(retryScript).toHaveBeenCalledWith('script-1'))
  })

  it('loads project history and renders evidence, score, and adoption status', async () => {
    render(<TopicRadarPage />)
    expect(await screen.findByText('城市里的微型菜园')).toBeInTheDocument()
    expect(screen.getByText('从阳台种植切入，讲清低成本入门方法。')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '来源：阳台种菜' })).toHaveAttribute('href', 'https://example.test/trend')
    expect(screen.getByText('82%')).toBeInTheDocument()
    expect(screen.getByText('已入库')).toBeInTheDocument()
  })

  it('starts a manual digest and submits a short-video script request', async () => {
    render(<TopicRadarPage />)
    await screen.findByText('城市里的微型菜园')
    fireEvent.click(screen.getByRole('button', { name: '手动分析' }))
    await waitFor(() => expect(run).toHaveBeenCalledWith('sub-1', expect.objectContaining({ request_key: expect.any(String) })))

    fireEvent.click(await screen.findByRole('button', { name: '生成短视频脚本' }))
    fireEvent.click(screen.getByRole('button', { name: '提交任务' }))
    await waitFor(() => expect(requestScript).toHaveBeenCalledWith('suggestion-1', {
      target_platform: 'douyin', duration_seconds: 60, aspect_ratio: '9:16',
    }))
  })
})
