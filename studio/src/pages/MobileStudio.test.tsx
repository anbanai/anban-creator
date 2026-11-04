import { QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createTestQueryClient } from '@/test/test-utils'
import { mockProjects } from '@/test/mocks/handlers'
import MobileStudioPage from './MobileStudioPage'
import MobileCreatePage from './MobileCreatePage'
import MobilePersonasPage from './MobilePersonasPage'
import { MobileNavigation } from '@/components/mobile/MobileNavigation'

const mocks = vi.hoisted(() => ({ list: vi.fn(), update: vi.fn(), create: vi.fn() }))
vi.mock('@/contexts/AuthContext', () => ({ useAuth: () => ({ user: { id: 'user-1', nickname: '测试用户', is_admin: false } }) }))
vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return { ...actual, api: { ...actual.api, projects: { ...actual.api.projects, list: mocks.list, update: mocks.update }, tasks: { ...actual.api.tasks, create: mocks.create } } }
})
function Location() { const location = useLocation(); return <output data-testid="location">{JSON.stringify({ path: location.pathname + location.search, state: location.state })}</output> }
function mount(page: React.ReactNode, path = '/mobile') {
  return render(<QueryClientProvider client={createTestQueryClient()}><MemoryRouter initialEntries={[path]}>{page}<Location /></MemoryRouter></QueryClientProvider>)
}
const persona = { ...mockProjects[0], id: 'p1', name: '咖啡主理人', agent_ids: ['wechat-article', 'seednote', 'video-edit'] }
beforeEach(() => { vi.clearAllMocks(); mocks.list.mockResolvedValue([persona]) })

describe('mobile core creation', () => {
  it('offers all four creation paths and a fresh persona interview without starting any task', async () => {
    mocks.list.mockResolvedValue([])
    mount(<MobileStudioPage />)
    await screen.findByText('先建立第一个人设')
    expect(screen.getByRole('link', { name: /公众号文章/ })).toHaveAttribute('href', '/mobile/create?type=wechat-article')
    expect(screen.getByRole('link', { name: /小红书图文/ })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /短视频剪辑/ })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /电商图片/ })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('link', { name: /聊一聊，建立你的人设/ }))
    expect(screen.getByTestId('location')).toHaveTextContent('"startNew":true')
    expect(mocks.create).not.toHaveBeenCalled()
  })
  it('passes the selected identity, exact note type and editable brief to a fresh conversation', async () => {
    mount(<MobileCreatePage />, '/mobile/create?project_id=p1&type=seednote')
    await screen.findByRole('option', { name: persona.name })
    fireEvent.change(screen.getByRole('textbox', { name: /你的想法/ }), { target: { value: '咖啡新手怎么选豆？' } })
    fireEvent.click(screen.getByRole('button', { name: '进入创作对话' }))
    expect(screen.getByTestId('location')).toHaveTextContent('"creationType":"seednote"')
    expect(screen.getByTestId('location')).toHaveTextContent('咖啡新手怎么选豆？')
    expect(screen.getByTestId('location')).toHaveTextContent('"projectId":"p1"')
    expect(mocks.update).not.toHaveBeenCalled()
    expect(mocks.create).not.toHaveBeenCalled()
  })
  it.each([
    ['商品主图', 'main_images'], ['详情图', 'detail_page'], ['白底图', 'main_images'], ['SKU 图', 'sku_images'],
  ])('hands %s to the existing ecommerce form with product modules and no paid submission', async (label, module) => {
    mocks.list.mockResolvedValue([{ ...persona, platform: 'ecommerce', agent_ids: ['ecommerce'] }])
    mount(<MobileCreatePage />, '/mobile/create?project_id=p1&type=ecommerce')
    await screen.findByRole('option', { name: persona.name })
    fireEvent.click(screen.getByRole('button', { name: new RegExp(label) }))
    fireEvent.change(screen.getByRole('textbox', { name: /产品与图片要求/ }), { target: { value: '真实咖啡杯，不改颜色' } })
    fireEvent.click(screen.getByRole('button', { name: '下一步，添加素材' }))
    const location = JSON.parse(screen.getByTestId('location').textContent!)
    expect(location.path).toBe('/tasks/new?project_id=p1&type=ecommerce')
    expect(location.state.mobileDraft.selectedModules[module]).toBeGreaterThan(0)
    expect(location.state.mobileDraft.prompt).toContain('真实咖啡杯，不改颜色')
    expect(mocks.create).not.toHaveBeenCalled()
  })
  it('requires explicit confirmation to switch to ecommerce and preserves the brief on cancel', async () => {
    mount(<MobileCreatePage />, '/mobile/create?project_id=p1&type=ecommerce')
    await screen.findByRole('option', { name: persona.name })
    fireEvent.change(screen.getByRole('textbox', { name: /产品与图片要求/ }), { target: { value: '保留我的要求' } })
    fireEvent.click(screen.getByRole('button', { name: '下一步，添加素材' }))
    expect(within(screen.getByRole('dialog')).getByRole('note')).toHaveTextContent('原画像保留')
    expect(mocks.update).not.toHaveBeenCalled()
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '取消' }))
    expect(screen.getByRole('textbox', { name: /产品与图片要求/ })).toHaveValue('保留我的要求')
    expect(mocks.create).not.toHaveBeenCalled()
  })
  it('continues only when saved types actually enable the requested creation', async () => {
    mocks.update.mockResolvedValue({ ...persona, platform: 'ecommerce', agent_ids: ['ecommerce'] })
    mount(<MobileCreatePage />, '/mobile/create?project_id=p1&type=ecommerce')
    await screen.findByRole('option', { name: persona.name })
    fireEvent.click(screen.getByRole('button', { name: '下一步，添加素材' }))
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '保存并继续创作' }))
    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent('/tasks/new?project_id=p1&type=ecommerce'))
    expect(mocks.update).toHaveBeenCalledWith('p1', { agent_ids: ['ecommerce'], platform: 'ecommerce' })
    expect(mocks.create).not.toHaveBeenCalled()
  })
  it('blocks an archived or removed selected identity instead of silently switching to another one', async () => {
    mount(<MobileCreatePage />, '/mobile/create?project_id=removed&type=seednote')
    expect(await screen.findByRole('alert')).toHaveTextContent('这个人设已不可用')
    expect(screen.getByRole('button', { name: '进入创作对话' })).toBeDisabled()
  })
  it('provides portrait details, continued interview and creation entry from the identity list', async () => {
    mount(<MobilePersonasPage />, '/mobile/personas')
    expect(await screen.findByRole('link', { name: /咖啡主理人/ })).toHaveAttribute('href', '/portraits/p1')
    expect(screen.getByRole('link', { name: '用这个人设创作' })).toHaveAttribute('href', '/mobile/create?project_id=p1')
    expect(mocks.list).toHaveBeenCalledWith({ status: 'active' })
  })
  it('keeps the correct mobile navigation selected on a task detail and on a profile interview', () => {
    mount(<MobileNavigation />, '/tasks/task-1')
    expect(screen.getByRole('link', { name: '作品' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('link', { name: '创作' })).not.toHaveAttribute('aria-current')
  })
})
