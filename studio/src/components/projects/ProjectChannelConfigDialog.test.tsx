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
    vi.spyOn(api.projects, 'listChannelConfigs').mockResolvedValue([
      { id: 'article', project_id: project.id, channel: 'wechat-article', config: { wechat_app_id: 'wx-article' } },
      { id: 'picture', project_id: project.id, channel: 'wechat-picture', config: { wechat_app_id: 'wx-picture' } },
    ])
    vi.spyOn(api.projects, 'upsertChannelConfig').mockResolvedValue({ id: 'article', project_id: project.id, channel: 'wechat-article', config: {} })
  })

  it('loads each channel independently and preserves an omitted secret', async () => {
    render(<ProjectChannelConfigDialog project={project} onClose={vi.fn()} />)
    await screen.findByDisplayValue('wx-article')
    fireEvent.click(screen.getByRole('button', { name: '公众号图文' }))
    await screen.findByDisplayValue('wx-picture')
    fireEvent.change(screen.getByLabelText('微信 AppID'), { target: { value: 'wx-picture-updated' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(api.projects.upsertChannelConfig).toHaveBeenCalledWith(project.id, 'wechat-picture', { wechat_app_id: 'wx-picture-updated' }))
    expect(api.projects.upsertChannelConfig).toHaveBeenCalledTimes(1)
  })

  it('can configure an empty project and retry a failed save without creating a project', async () => {
    vi.mocked(api.projects.listChannelConfigs).mockResolvedValue([])
    vi.mocked(api.projects.upsertChannelConfig).mockRejectedValueOnce(new Error('temporary failure'))
    const onClose = vi.fn()
    render(<ProjectChannelConfigDialog project={project} onClose={onClose} />)
    await screen.findByLabelText('微信 AppID')
    fireEvent.change(screen.getByLabelText('微信 AppID'), { target: { value: 'wx-new' } })
    fireEvent.change(screen.getByLabelText('微信 AppSecret'), { target: { value: 'test-secret' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(screen.getByRole('button', { name: '保存' })).toBeEnabled())
    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByLabelText('微信 AppSecret')).toHaveValue('test-secret')
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(screen.getByLabelText('微信 AppSecret')).toHaveValue(''))
    expect(onClose).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: /^关闭$/ }))
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(api.projects.upsertChannelConfig).toHaveBeenLastCalledWith(project.id, 'wechat-article', { wechat_app_id: 'wx-new', wechat_secret: 'test-secret' })
  })

  it('waits for a fresh read before editing cached credentials', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    client.setQueryData(['project-channel-configs', project.id], [
      { id: 'article', project_id: project.id, channel: 'wechat-article', config: { wechat_app_id: 'wx-stale' } },
    ])
    let resolveRead!: (value: Awaited<ReturnType<typeof api.projects.listChannelConfigs>>) => void
    vi.mocked(api.projects.listChannelConfigs).mockReturnValueOnce(new Promise((resolve) => { resolveRead = resolve }))
    render(<QueryClientProvider client={client}><ProjectChannelConfigDialog project={project} onClose={vi.fn()} /></QueryClientProvider>)
    expect(screen.queryByLabelText('微信 AppID')).not.toBeInTheDocument()
    await act(async () => resolveRead([
      { id: 'article', project_id: project.id, channel: 'wechat-article', config: { wechat_app_id: 'wx-fresh' } },
    ]))
    await screen.findByDisplayValue('wx-fresh')
    fireEvent.change(screen.getByLabelText('微信 AppSecret'), { target: { value: 'test-secret' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(api.projects.upsertChannelConfig).toHaveBeenCalledWith(project.id, 'wechat-article', { wechat_app_id: 'wx-fresh', wechat_secret: 'test-secret' }))
    client.clear()
  })

  it('preserves unsaved changes in another channel and confirms before discarding', async () => {
    const onClose = vi.fn()
    render(<ProjectChannelConfigDialog project={project} onClose={onClose} />)
    await screen.findByDisplayValue('wx-article')
    fireEvent.change(screen.getByLabelText('微信 AppID'), { target: { value: 'wx-unsaved' } })
    fireEvent.click(screen.getByRole('button', { name: '公众号图文' }))
    fireEvent.click(screen.getByRole('button', { name: /^保存$/ }))
    await waitFor(() => expect(screen.getByRole('button', { name: /^保存$/ })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '公众号文章' }))
    expect(screen.getByLabelText('微信 AppID')).toHaveValue('wx-unsaved')
    fireEvent.click(screen.getByRole('button', { name: /^关闭$/ }))
    expect(onClose).not.toHaveBeenCalled()
    fireEvent.click(await screen.findByRole('button', { name: '放弃修改' }))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('blocks editing when existing settings cannot be loaded', async () => {
    vi.mocked(api.projects.listChannelConfigs).mockRejectedValue(new Error('unavailable'))
    render(<ProjectChannelConfigDialog project={project} onClose={vi.fn()} />)
    await screen.findByText(/加载失败/)
    expect(screen.queryByLabelText('微信 AppID')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '保存' })).not.toBeInTheDocument()
  })
})
