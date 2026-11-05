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
    expect(screen.getByRole('heading', { name: '公众号设置' })).toBeInTheDocument()
    expect(screen.getByRole('img', { name: '微信平台' })).toBeInTheDocument()
    expect(screen.getByText('公众号长文与贴图内容共用此账号。')).toBeInTheDocument()
    expect(screen.queryByRole('group', { name: '发布渠道' })).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('微信 AppID'), { target: { value: 'wx-updated' } })
    expect(screen.getByLabelText('微信 AppSecret')).toBeRequired()
    fireEvent.change(screen.getByLabelText('微信 AppSecret'), { target: { value: 'secret-updated' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(api.projects.upsertWechatPlatformConfig).toHaveBeenCalledWith(project.id, { app_id: 'wx-updated', secret: 'secret-updated', automatic_delivery_enabled: true, delivery_mode: 'draft' }))
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
    expect(api.projects.upsertWechatPlatformConfig).toHaveBeenLastCalledWith(project.id, { app_id: 'wx-new', secret: 'test-secret', automatic_delivery_enabled: true, delivery_mode: 'draft' })
  })

  it('defaults legacy configuration without automatic delivery to manual copy', async () => {
    vi.mocked(api.projects.getWechatPlatformConfig).mockResolvedValue({ project_id: project.id, platform: 'wechat', app_id: 'wx-shared', secret_configured: true, credentials_conflict: false } as Awaited<ReturnType<typeof api.projects.getWechatPlatformConfig>>)
    vi.mocked(api.projects.upsertWechatPlatformConfig).mockResolvedValue({ project_id: project.id, platform: 'wechat', app_id: 'wx-shared', secret_configured: true, automatic_delivery_enabled: false, credentials_conflict: false })
    render(<ProjectChannelConfigDialog project={project} onClose={vi.fn()} />)
    await screen.findByDisplayValue('wx-shared')
    expect(screen.getByRole('button', { name: /手动复制/ })).toHaveAttribute('aria-pressed', 'true')
  })

  it('makes QR authorization the primary path and explains the draft default', async () => {
    vi.mocked(api.projects.getWechatPlatformConfig).mockResolvedValue({
      project_id: project.id,
      platform: 'wechat',
      app_id: 'wx-legacy',
      secret_configured: true,
      credentials_conflict: false,
      automatic_delivery_enabled: false,
      authorization_enabled: true,
      connection: {
        authorization_enabled: true,
        authorization_record_exists: false,
        connected: false,
        status: 'disconnected',
        can_create_draft: false,
        can_upload_material: false,
      },
    })
    render(<ProjectChannelConfigDialog project={project} onClose={vi.fn()} />)
    expect(await screen.findByRole('button', { name: /扫码绑定公众号/ })).toBeInTheDocument()
    expect(screen.getByText('绑定后可启用草稿箱交付；不会发布或群发。')).toBeInTheDocument()
    expect(screen.getByText('使用存量 AppID 配置').closest('details')).not.toHaveAttribute('open')
  })

  it('saves the project default delivery mode without publishing automatically', async () => {
    vi.mocked(api.projects.getWechatPlatformConfig).mockResolvedValue({
      project_id: project.id, platform: 'wechat', app_id: '', secret_configured: false,
      credentials_conflict: false, automatic_delivery_enabled: true, delivery_mode: 'draft', authorization_enabled: true,
      connection: { authorization_enabled: true, authorization_record_exists: true, connected: true, status: 'connected', can_create_draft: true, can_upload_material: true, can_formally_publish: true },
    })
    render(<ProjectChannelConfigDialog project={project} onClose={vi.fn()} />)
    fireEvent.click(await screen.findByRole('button', { name: /正式发布/ }))
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(api.projects.upsertWechatPlatformConfig).toHaveBeenCalledWith(project.id, { automatic_delivery_enabled: true, delivery_mode: 'direct_publish' }))
    expect(screen.getByText(/手动复制适合需要自己把关/)).toBeInTheDocument()
  })

  it('requires both draft and formal publish capabilities to select formal delivery', async () => {
    vi.mocked(api.projects.getWechatPlatformConfig).mockResolvedValue({
      project_id: project.id, platform: 'wechat', app_id: '', secret_configured: false,
      credentials_conflict: false, automatic_delivery_enabled: true, delivery_mode: 'draft', authorization_enabled: true,
      connection: { authorization_enabled: true, authorization_record_exists: true, connected: true, status: 'connected', can_create_draft: false, can_upload_material: false, can_formally_publish: true },
    })
    render(<ProjectChannelConfigDialog project={project} onClose={vi.fn()} />)
    expect(await screen.findByRole('button', { name: /正式发布/ })).toBeDisabled()
    expect(screen.getByText(/缺少草稿箱权限/)).toBeInTheDocument()
  })

  it('disables draft delivery after authorization is revoked and does not show legacy secrets', async () => {
    vi.mocked(api.projects.getWechatPlatformConfig).mockResolvedValue({
      project_id: project.id,
      platform: 'wechat',
      app_id: 'wx-legacy',
      secret_configured: true,
      credentials_conflict: false,
      automatic_delivery_enabled: true,
      authorization_enabled: true,
      connection: {
        authorization_enabled: true,
        authorization_record_exists: true,
        connected: false,
        status: 'revoked',
        can_create_draft: false,
        can_upload_material: false,
      },
    })
    render(<ProjectChannelConfigDialog project={project} onClose={vi.fn()} />)
    expect(await screen.findByText(/微信已撤销授权/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /发布到草稿箱/ })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByLabelText('微信 AppSecret')).not.toBeInTheDocument()
  })

  it('creates the authorization state and opens the WeChat QR page in the click-created window', async () => {
    vi.mocked(api.projects.getWechatPlatformConfig).mockResolvedValue({
      project_id: project.id,
      platform: 'wechat',
      app_id: '',
      secret_configured: false,
      credentials_conflict: false,
      automatic_delivery_enabled: false,
      authorization_enabled: true,
      connection: {
        authorization_enabled: true,
        authorization_record_exists: false,
        connected: false,
        status: 'disconnected',
        can_create_draft: false,
        can_upload_material: false,
      },
    })
    const replace = vi.fn()
    const popup = { closed: false, opener: window, location: { replace }, close: vi.fn() } as unknown as Window
    vi.spyOn(window, 'open').mockReturnValue(popup)
    vi.spyOn(api.projects, 'startWechatAuthorization').mockResolvedValue({
      authorization_url: 'https://mp.weixin.qq.com/componentloginpage?state=opaque',
      expires_at: '2026-10-26T01:00:00Z',
    })
    render(<ProjectChannelConfigDialog project={project} onClose={vi.fn()} />)
    fireEvent.click(await screen.findByRole('button', { name: /扫码绑定公众号/ }))
    await waitFor(() => expect(api.projects.startWechatAuthorization).toHaveBeenCalledWith(project.id))
    expect(replace).toHaveBeenCalledWith('https://mp.weixin.qq.com/componentloginpage?state=opaque')
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
    await waitFor(() => expect(api.projects.upsertWechatPlatformConfig).toHaveBeenCalledWith(project.id, { app_id: 'wx-fresh', secret: 'test-secret', automatic_delivery_enabled: true, delivery_mode: 'draft' }))
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
