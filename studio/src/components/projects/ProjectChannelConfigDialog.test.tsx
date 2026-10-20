import { act, fireEvent, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'
import { ProjectChannelConfigDialog } from './ProjectChannelConfigDialog'

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))
const project = { id: 'shared-project', name: '共享品牌' }

describe('ProjectChannelConfigDialog', () => {
  beforeEach(() => {
    vi.spyOn(api.projects, 'getWechatPlatformConfig').mockResolvedValue({ project_id: project.id, platform: 'wechat', app_id: 'wx-shared', secret_configured: true, automatic_delivery_enabled: true, credentials_conflict: false })
    vi.spyOn(api.projects, 'upsertWechatPlatformConfig').mockResolvedValue({ project_id: project.id, platform: 'wechat', app_id: 'wx-updated', secret_configured: true, automatic_delivery_enabled: true, credentials_conflict: false })
  })

  it('shows one WeChat platform and saves shared credentials for both content channels', async () => {
    render(<ProjectChannelConfigDialog project={project} onClose={vi.fn()} />)
    await screen.findByDisplayValue('wx-shared')
    expect(screen.getByRole('heading', { name: '配置自动发布' })).toBeInTheDocument()
    expect(screen.getByRole('img', { name: '微信平台' })).toBeInTheDocument()
    expect(screen.getByText('公众号长文与贴图内容共用此账号。')).toBeInTheDocument()
    expect(screen.queryByRole('group', { name: '发布渠道' })).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('微信 AppID'), { target: { value: 'wx-updated' } })
    expect(screen.getByLabelText('微信 AppSecret')).toBeRequired()
    fireEvent.change(screen.getByLabelText('微信 AppSecret'), { target: { value: 'secret-updated' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(api.projects.upsertWechatPlatformConfig).toHaveBeenCalledWith(project.id, { app_id: 'wx-updated', secret: 'secret-updated', automatic_delivery_enabled: true }))
    expect(api.projects.upsertWechatPlatformConfig).toHaveBeenCalledTimes(1)
  })

  it('requires a fresh secret when legacy WeChat channel credentials conflict', async () => {
    vi.mocked(api.projects.getWechatPlatformConfig).mockResolvedValue({ project_id: project.id, platform: 'wechat', app_id: '', secret_configured: true, automatic_delivery_enabled: true, credentials_conflict: true })
    render(<ProjectChannelConfigDialog project={project} onClose={vi.fn()} />)
    await screen.findByLabelText('微信 AppID')
    expect(screen.getByLabelText('微信 AppSecret')).toBeRequired()
    expect(screen.getByText('文章与贴图的旧账号配置不一致，请重新填写要统一使用的微信账号和密钥。')).toBeInTheDocument()
  })

  it('keeps the platform incomplete after an AppID change until the new credentials are saved', async () => {
    render(<ProjectChannelConfigDialog project={project} onClose={vi.fn()} />)
    await screen.findByDisplayValue('wx-shared')
    expect(screen.getByText('已就绪')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('微信 AppID'), { target: { value: 'wx-new' } })
    expect(screen.getByText('待完善')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('微信 AppSecret'), { target: { value: 'secret-new' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => expect(screen.getByText('已就绪')).toBeInTheDocument())
  })

  it('can configure an empty project and retry a failed save without creating a project', async () => {
    vi.mocked(api.projects.getWechatPlatformConfig).mockResolvedValue({ project_id: project.id, platform: 'wechat', app_id: '', secret_configured: false, automatic_delivery_enabled: true, credentials_conflict: false })
    vi.mocked(api.projects.upsertWechatPlatformConfig).mockRejectedValueOnce(new Error('temporary failure'))
    const onClose = vi.fn()
    render(<ProjectChannelConfigDialog project={project} onClose={onClose} />)
    await screen.findByLabelText('微信 AppID')
    fireEvent.change(screen.getByLabelText('微信 AppID'), { target: { value: 'wx-new' } })
    expect(screen.getByLabelText('微信 AppSecret')).toBeRequired()
    expect(screen.getByRole('button', { name: '保存' })).toBeDisabled()
    fireEvent.change(screen.getByLabelText('微信 AppSecret'), { target: { value: 'test-secret' } })
    expect(screen.getByRole('button', { name: '保存' })).toBeEnabled()
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(screen.getByRole('button', { name: '保存' })).toBeEnabled())
    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByLabelText('微信 AppSecret')).toHaveValue('test-secret')
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(screen.getByLabelText('微信 AppSecret')).toHaveValue(''))
    expect(screen.getByText(/密钥已安全保存/)).toBeInTheDocument()
    expect(onClose).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: /^关闭$/ }))
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(api.projects.upsertWechatPlatformConfig).toHaveBeenLastCalledWith(project.id, { app_id: 'wx-new', secret: 'test-secret', automatic_delivery_enabled: true })
  })

  it('keeps automatic delivery off when legacy configuration omits the switch', async () => {
    vi.mocked(api.projects.getWechatPlatformConfig).mockResolvedValue({ project_id: project.id, platform: 'wechat', app_id: 'wx-shared', secret_configured: true, credentials_conflict: false } as Awaited<ReturnType<typeof api.projects.getWechatPlatformConfig>>)
    vi.mocked(api.projects.upsertWechatPlatformConfig).mockResolvedValue({ project_id: project.id, platform: 'wechat', app_id: 'wx-shared', secret_configured: true, automatic_delivery_enabled: false, credentials_conflict: false })
    render(<ProjectChannelConfigDialog project={project} onClose={vi.fn()} />)
    await screen.findByDisplayValue('wx-shared')
    expect(screen.getByLabelText('允许自动交付')).not.toBeChecked()
  })

  it('waits for a fresh read before editing cached credentials', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    client.setQueryData(['project-wechat-platform-config', project.id], { project_id: project.id, platform: 'wechat', app_id: 'wx-stale', secret_configured: true, automatic_delivery_enabled: true, credentials_conflict: false })
    let resolveRead!: (value: Awaited<ReturnType<typeof api.projects.getWechatPlatformConfig>>) => void
    vi.mocked(api.projects.getWechatPlatformConfig).mockReturnValueOnce(new Promise((resolve) => { resolveRead = resolve }))
    render(<QueryClientProvider client={client}><ProjectChannelConfigDialog project={project} onClose={vi.fn()} /></QueryClientProvider>)
    expect(screen.queryByLabelText('微信 AppID')).not.toBeInTheDocument()
    await act(async () => resolveRead({ project_id: project.id, platform: 'wechat', app_id: 'wx-fresh', secret_configured: true, automatic_delivery_enabled: true, credentials_conflict: false }))
    await screen.findByDisplayValue('wx-fresh')
    fireEvent.change(screen.getByLabelText('微信 AppSecret'), { target: { value: 'test-secret' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(api.projects.upsertWechatPlatformConfig).toHaveBeenCalledWith(project.id, { app_id: 'wx-fresh', secret: 'test-secret', automatic_delivery_enabled: true }))
    client.clear()
  })

  it('confirms before discarding unsaved platform changes', async () => {
    const onClose = vi.fn()
    render(<ProjectChannelConfigDialog project={project} onClose={onClose} />)
    await screen.findByDisplayValue('wx-shared')
    fireEvent.change(screen.getByLabelText('微信 AppID'), { target: { value: 'wx-unsaved' } })
    fireEvent.click(screen.getByRole('button', { name: /^关闭$/ }))
    expect(onClose).not.toHaveBeenCalled()
    fireEvent.click(await screen.findByRole('button', { name: '放弃修改' }))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('blocks editing when existing settings cannot be loaded', async () => {
    vi.mocked(api.projects.getWechatPlatformConfig).mockRejectedValue({ response: { data: { code: 40000, msg: '微信 AppID 和 AppSecret 配置无效' } } })
    render(<ProjectChannelConfigDialog project={project} onClose={vi.fn()} />)
    await screen.findByText(/加载失败/)
    expect(screen.getByText(/微信 AppID 和 AppSecret 配置无效/)).toBeInTheDocument()
    expect(screen.queryByLabelText('微信 AppID')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '保存' })).not.toBeInTheDocument()
  })
})
