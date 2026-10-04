import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import PlansPage from './PlansPage'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'
import { mockAgentPackCatalog, mockPlans, mockProjects } from '@/test/mocks/handlers'
import type { ReferenceMaterialInputProps } from '@/components/ReferenceMaterialInput'

const harness = vi.hoisted(() => ({ error: vi.fn(), materials: undefined as ReferenceMaterialInputProps | undefined }))
vi.mock('sonner', () => ({ toast: { error: harness.error, success: vi.fn() } }))
vi.mock('@/components/ReferenceMaterialInput', () => ({
  ReferenceMaterialInput: (props: ReferenceMaterialInputProps) => { harness.materials = props; return <div /> },
}))
vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return { ...actual, api: { ...actual.api,
    projects: { ...actual.api.projects, list: vi.fn() },
    plans: { ...actual.api.plans, list: vi.fn(), create: vi.fn(), update: vi.fn(), pause: vi.fn(), resume: vi.fn(), delete: vi.fn(), scheduleRecommendation: vi.fn() },
    agentPacks: { ...actual.api.agentPacks, list: vi.fn() },
    agentProfiles: { ...actual.api.agentProfiles, list: vi.fn() },
    imageCapabilities: { ...actual.api.imageCapabilities, list: vi.fn() },
  } }
})
async function openCreate() {
  fireEvent.click((await screen.findAllByRole('button', { name: '新建计划' }))[0])
  return screen.findByRole('dialog', { name: '新建计划' })
}
async function selectProject(dialog: HTMLElement, name: string) {
  fireEvent.click(within(dialog).getByRole('combobox', { name: /^项目/ }))
  fireEvent.click(await screen.findByRole('option', { name: new RegExp(name) }))
}
beforeEach(() => {
  vi.clearAllMocks()
  window.history.pushState({}, '', '/plans')
  harness.materials = undefined
  vi.mocked(api.projects.list).mockResolvedValue([mockProjects[0], { ...mockProjects[0], id: 'second-project', name: '第二账号', platform: 'seednote' }])
  vi.mocked(api.plans.list).mockResolvedValue({ items: [], total: 0 })
  vi.mocked(api.plans.create).mockResolvedValue(mockPlans.items[0])
  vi.mocked(api.plans.update).mockResolvedValue(mockPlans.items[0])
  vi.mocked(api.plans.scheduleRecommendation).mockResolvedValue({ time: '12:07', timezone: 'Asia/Shanghai', granularity_minutes: 15, load_balanced: true })
  vi.mocked(api.agentPacks.list).mockResolvedValue(mockAgentPackCatalog)
  vi.mocked(api.agentProfiles.list).mockResolvedValue([{ id: 'effective', display_name: '性价比', provider: 'deepseek', model_name: 'deepseek-flash', description: '日常创作', min_tier: 'free', available: true }])
  vi.mocked(api.imageCapabilities.list).mockResolvedValue({ tier: 'pro', default_capability: 'standard', items: [{ key: 'standard', display_name: '标准图像', price_available: true, enabled: true }] })
})
describe('PlansPage multi-output plans', () => {
  it('starts empty and required, and orders the minimal form with advanced settings collapsed', async () => {
    render(<PlansPage />)
    const dialog = await openCreate()
    for (const label of ['公众号文章', '种草笔记', '公众号贴图']) expect(await within(dialog).findByRole('button', { name: label })).toHaveAttribute('aria-pressed', 'false')
    expect(within(dialog).getByRole('button', { name: '创建' })).toBeDisabled()
    expect(Array.from(dialog.querySelectorAll('h3')).map((node) => node.textContent)).toEqual(['输出类型', '项目', '创作提示', '排期', '执行配置', '共享图片设置'])
    expect(dialog.querySelector('details')).not.toHaveAttribute('open')
    expect(dialog).not.toHaveTextContent(/Agent 参数|任务类型|Schema|视频类型|主参考视频链接/)
  })
  it('requires explicit output selection even when opened from a typed project link', async () => {
    window.history.pushState({}, '', `/plans?create=true&type=wechat-article&project_id=${mockProjects[0].id}`)
    render(<PlansPage />)
    const dialog = await screen.findByRole('dialog', { name: '新建计划' })
    expect(await within(dialog).findByRole('button', { name: '公众号文章' })).toHaveAttribute('aria-pressed', 'false')
    expect(within(dialog).getByRole('button', { name: '创建' })).toBeDisabled()
  })
  it('loads new eligible packs dynamically and excludes incomplete or non-plan packs', async () => {
    const base = mockAgentPackCatalog.packs[0]
    vi.mocked(api.agentPacks.list).mockResolvedValue({ packs: [
      ...mockAgentPackCatalog.packs,
      { ...base, id: 'new-output', display_name: '新增输出', channel: 'new-channel' },
      { ...base, id: 'plugin-only', display_name: '插件输出', kind: 'plugin' },
      { ...base, id: 'no-channel', display_name: '缺少渠道', channel: '' },
      { ...base, id: 'bad-kind', display_name: '错误任务', plan_task_kind: 'not-bound' },
      { ...base, id: 'no-plan', display_name: '仅任务', surfaces: ['task'] },
    ] })
    render(<PlansPage />)
    const dialog = await openCreate()
    expect(await within(dialog).findByRole('button', { name: '新增输出' })).toBeInTheDocument()
    for (const name of ['插件输出', '缺少渠道', '错误任务', '仅任务', '视频生成']) expect(within(dialog).queryByRole('button', { name })).not.toBeInTheDocument()
  })
  it('preserves multiple outputs across project platforms and submits only shared fields', async () => {
    render(<PlansPage />)
    const dialog = await openCreate()
    fireEvent.click(await within(dialog).findByRole('button', { name: '公众号文章' }))
    fireEvent.click(within(dialog).getByRole('button', { name: '公众号贴图' }))
    await selectProject(dialog, mockProjects[0].name)
    await selectProject(dialog, '第二账号')
    expect(within(dialog).getByRole('button', { name: '公众号文章' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(dialog).getByRole('button', { name: '公众号贴图' })).toHaveAttribute('aria-pressed', 'true')
    fireEvent.change(within(dialog).getByPlaceholderText('描述每次创作的方向、要求和素材使用方式'), { target: { value: '  每周介绍新品  ' } })
    await waitFor(() => expect(within(dialog).getByRole('button', { name: '创建' })).toBeEnabled())
    fireEvent.click(within(dialog).getByRole('button', { name: '创建' }))
    await waitFor(() => expect(api.plans.create).toHaveBeenCalledTimes(1))
    expect(JSON.parse(JSON.stringify(vi.mocked(api.plans.create).mock.calls[0][0]))).toEqual({
      project_id: 'second-project', agent_ids: ['wechat-article', 'wechat-picture'], execution_profile: 'effective',
      cron_expr: '7 12 * * 1,3,5', prompt: '每周介绍新品', image_ratio: mockProjects[0].image_ratio,
      skip_reference_image: false, reference_image: null, input_attachments: [], watermark: false,
    })
  })
  it('disables submission after deselecting every output', async () => {
    render(<PlansPage />)
    const dialog = await openCreate()
    const output = await within(dialog).findByRole('button', { name: '种草笔记' })
    fireEvent.click(output)
    await selectProject(dialog, mockProjects[0].name)
    await waitFor(() => expect(within(dialog).getByRole('button', { name: '创建' })).toBeEnabled())
    fireEvent.click(output)
    expect(within(dialog).getByRole('button', { name: '创建' })).toBeDisabled()
    expect(api.plans.create).not.toHaveBeenCalled()
  })
  it('shows output labels and restores the saved selection when editing', async () => {
    const plan = { ...mockPlans.items[0], prompt: '多种输出', agent_ids: ['seednote', 'wechat-picture'] }
    vi.mocked(api.plans.list).mockResolvedValue({ items: [plan], total: 1 })
    render(<PlansPage />)
    expect(await screen.findByText('种草笔记')).toBeInTheDocument()
    expect(await screen.findByText('公众号贴图')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '编辑' }))
    const dialog = await screen.findByRole('dialog', { name: '编辑计划' })
    expect(within(dialog).getByRole('button', { name: '种草笔记' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(dialog).getByRole('button', { name: '公众号贴图' })).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(within(dialog).getByRole('button', { name: '公众号文章' }))
    fireEvent.click(within(dialog).getByRole('button', { name: '保存' }))
    await waitFor(() => expect(api.plans.update).toHaveBeenCalledWith(plan.id, expect.objectContaining({ agent_ids: ['seednote', 'wechat-picture', 'wechat-article'] })))
    expect(api.plans.scheduleRecommendation).not.toHaveBeenCalled()
  })
  it.each([false, true])('shows the saved reference and submits its selection after removal=%s', async (remove) => {
    const reference = { asset_id: '44444444-4444-4444-8444-444444444444', file_name: 'cover.png', content_type: 'image/png', size: 9, download_url: 'https://signed.example/cover.png', download_expires_at: '2026-10-04T10:00:00Z' }
    const plan = { ...mockPlans.items[0], agent_ids: ['seednote'], reference_image: reference, skip_reference_image: false }
    vi.mocked(api.plans.list).mockResolvedValue({ items: [plan], total: 1 })
    render(<PlansPage />)
    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    const dialog = await screen.findByRole('dialog', { name: '编辑计划' })
    fireEvent.click(within(dialog).getByText('高级设置'))
    expect(within(dialog).getByRole('img', { name: '参考图' })).toHaveAttribute('src', reference.download_url)
    if (remove) fireEvent.click(within(dialog).getByRole('button', { name: '移除参考图' }))
    fireEvent.click(within(dialog).getByRole('button', { name: '保存' }))
    await waitFor(() => expect(api.plans.update).toHaveBeenCalledWith(plan.id, expect.objectContaining({ reference_image: remove ? null : { asset_id: reference.asset_id } })))
  })

  it('keeps manual schedule changes when the recommendation resolves later', async () => {
    let resolveRecommendation!: (value: Awaited<ReturnType<typeof api.plans.scheduleRecommendation>>) => void
    vi.mocked(api.plans.scheduleRecommendation).mockReturnValue(new Promise((resolve) => { resolveRecommendation = resolve }))
    render(<PlansPage />)
    const dialog = await openCreate()
    fireEvent.click(await within(dialog).findByRole('button', { name: '种草笔记' }))
    await selectProject(dialog, mockProjects[0].name)
    fireEvent.click(within(dialog).getByRole('button', { name: '每天' }))
    await act(async () => resolveRecommendation({ time: '22:22', timezone: 'Asia/Shanghai', granularity_minutes: 15, load_balanced: true }))
    fireEvent.click(within(dialog).getByRole('button', { name: '创建' }))
    await waitFor(() => expect(api.plans.create).toHaveBeenCalledWith(expect.objectContaining({ cron_expr: '0 9 * * *' })))
  })
  it('blocks unresolved uploads and reports save failures', async () => {
    vi.mocked(api.plans.create).mockRejectedValue(new Error('network failed'))
    render(<PlansPage />)
    const dialog = await openCreate()
    fireEvent.click(await within(dialog).findByRole('button', { name: '种草笔记' }))
    await selectProject(dialog, mockProjects[0].name)
    act(() => harness.materials?.onUploadingChange?.(true))
    expect(within(dialog).getByRole('button', { name: '创建' })).toBeDisabled()
    act(() => { harness.materials?.onUploadingChange?.(false); harness.materials?.onFailuresChange?.(true) })
    expect(within(dialog).getByRole('button', { name: '创建' })).toBeDisabled()
    act(() => harness.materials?.onFailuresChange?.(false))
    await waitFor(() => expect(within(dialog).getByRole('button', { name: '创建' })).toBeEnabled())
    fireEvent.click(within(dialog).getByRole('button', { name: '创建' }))
    await waitFor(() => expect(harness.error).toHaveBeenCalled())
    expect(dialog).toBeInTheDocument()
  })
  it('does not invent outputs when the catalog is empty', async () => {
    vi.mocked(api.agentPacks.list).mockResolvedValue({ packs: [] })
    render(<PlansPage />)
    const dialog = await openCreate()
    expect(await within(dialog).findByText('暂时没有可用于计划的输出类型。')).toBeInTheDocument()
    expect(within(dialog).queryByRole('button', { name: '公众号文章' })).not.toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '创建' })).toBeDisabled()
  })
})
