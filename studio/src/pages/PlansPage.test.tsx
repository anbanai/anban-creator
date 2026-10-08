import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import PlansPage from './PlansPage'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'
import { mockAgentPackCatalog, mockPlans, mockProjects } from '@/test/mocks/handlers'
import type { Plan } from '@/types'


const harness = vi.hoisted(() => ({ error: vi.fn(), upload: vi.fn() }))
vi.mock('sonner', () => ({ toast: { error: harness.error, success: vi.fn() } }))
vi.mock('@/lib/direct-upload', async () => ({
  ...await vi.importActual<typeof import('@/lib/direct-upload')>('@/lib/direct-upload'),
  uploadToOSS: harness.upload,
}))
vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return { ...actual, api: { ...actual.api,
    projects: { ...actual.api.projects, list: vi.fn(), platformConfigs: vi.fn() },
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
  vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:plan-portrait-preview')
  vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
  window.history.pushState({}, '', '/plans')
  harness.upload.mockReset()
  vi.mocked(api.projects.platformConfigs).mockResolvedValue([
    { id: 'wechat', supported_image_ratios: ['16:9', '4:3', '1:1', '3:4'] },
    { id: 'seednote', supported_image_ratios: ['3:4', '1:1', '4:3'] },
  ] as Awaited<ReturnType<typeof api.projects.platformConfigs>>)
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
  it('only offers shared valid ratios and normalizes project defaults for multiple outputs', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([{ ...mockProjects[0], image_ratio: '16:9' }])
    render(<PlansPage />)
    const dialog = await openCreate()
    fireEvent.click(await within(dialog).findByRole('button', { name: '公众号文章' }))
    await selectProject(dialog, mockProjects[0].name)
    fireEvent.click(within(dialog).getByRole('button', { name: '种草笔记' }))
    fireEvent.click(within(dialog).getByRole('button', { name: /创作参数：/ }))
    const ratios = await screen.findByRole('group', { name: '图片比例' })
    expect(within(ratios).queryByRole('button', { name: '16:9' })).not.toBeInTheDocument()
    expect(within(ratios).queryByRole('button', { name: '9:16' })).not.toBeInTheDocument()
    expect(within(ratios).getByRole('button', { name: '智能适配' })).toHaveAttribute('aria-pressed', 'true')
    fireEvent.keyDown(ratios, { key: 'Escape' })
    await waitFor(() => expect(within(dialog).getByRole('button', { name: '创建' })).toBeEnabled())
    fireEvent.click(within(dialog).getByRole('button', { name: '创建' }))
    await waitFor(() => expect(api.plans.create).toHaveBeenCalledWith(expect.objectContaining({
      agent_ids: ['wechat-article', 'seednote'], image_ratio: 'auto',
    })))
  })
  it('starts empty and required, and orders the minimal form with advanced settings collapsed', async () => {
    render(<PlansPage />)
    const dialog = await openCreate()
    for (const label of ['公众号文章', '种草笔记', '公众号贴图']) expect(await within(dialog).findByRole('button', { name: label })).toHaveAttribute('aria-pressed', 'false')
    expect(within(dialog).getByRole('button', { name: '创建' })).toBeDisabled()
    expect(Array.from(dialog.querySelectorAll('h3')).map((node) => node.textContent)).toEqual(['创作类型', '创作提示', '排期', '执行设置'])
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
    for (const name of ['插件输出', '缺少渠道']) expect(within(dialog).queryByRole('button', { name })).not.toBeInTheDocument()
    for (const name of ['错误任务', '仅任务', '视频生成', '视频复刻', '白板动画']) expect(within(dialog).getByRole('button', { name })).toBeDisabled()
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
      skip_reference_image: false, reference_image: null, portrait_reference_image: null, cover_use_portrait: false, input_attachments: [], watermark: false,
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
  it('shows output Agent icons and restores the saved selection when editing', async () => {
    const plan = { ...mockPlans.items[0], prompt: '多种输出', agent_ids: ['seednote', 'wechat-picture'] }
    vi.mocked(api.plans.list).mockResolvedValue({ items: [plan], total: 1 })
    render(<PlansPage />)
    await screen.findByText('多种输出')
    const planCard = document.querySelector('[data-plan-id="plan-1"]')
    expect(planCard).toBeInTheDocument()
    expect(planCard?.querySelector('[data-agent-id="seednote"]')).toBeInTheDocument()
    expect(planCard?.querySelector('[data-agent-id="wechat-picture"]')).toBeInTheDocument()
    expect(planCard?.querySelectorAll('[data-slot="agent-icon"]')).toHaveLength(2)
    expect(planCard).not.toHaveTextContent('种草笔记')
    expect(planCard).not.toHaveTextContent('公众号贴图')
    fireEvent.click(screen.getByRole('button', { name: '编辑计划：多种输出' }))
    const dialog = await screen.findByRole('dialog', { name: '编辑计划' })
    expect(within(dialog).getByRole('button', { name: '种草笔记' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(dialog).getByRole('button', { name: '公众号贴图' })).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(within(dialog).getByRole('button', { name: '公众号文章' }))
    await waitFor(() => expect(within(dialog).getByRole('button', { name: '保存' })).toBeEnabled())
    fireEvent.click(within(dialog).getByRole('button', { name: '保存' }))
    await waitFor(() => expect(api.plans.update).toHaveBeenCalledWith(plan.id, expect.objectContaining({ agent_ids: ['seednote', 'wechat-picture', 'wechat-article'] })))
    expect(api.plans.scheduleRecommendation).not.toHaveBeenCalled()
  })

  it('groups schedule cards by active and paused state with cadence and next-run details', async () => {
    const activePlan: Plan = { ...mockPlans.items[0], id: 'active-plan', title: '活跃计划', prompt: '活跃内容', status: 'active', next_run_at: '2026-10-08T01:00:00Z' }
    const pausedPlan: Plan = { ...mockPlans.items[0], id: 'paused-plan', title: '暂停计划', prompt: '暂停内容', status: 'paused', next_run_at: '' }
    vi.mocked(api.plans.list).mockResolvedValue({ items: [activePlan, pausedPlan], total: 2 })
    render(<PlansPage />)

    expect(await screen.findByRole('region', { name: '运行中的计划' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '已暂停的计划' })).toBeInTheDocument()
    expect(screen.getByTestId('plan-card-active-plan')).toHaveTextContent('下次运行')
    expect(screen.getByTestId('plan-card-active-plan')).toHaveTextContent('每')
    expect(screen.getByTestId('plan-card-paused-plan')).toHaveTextContent('已暂停')
    const activeCard = within(screen.getByTestId('plan-card-active-plan'))
    for (const name of ['编辑计划：活跃内容', '查看任务：活跃内容', '暂停计划：活跃内容', '删除计划：活跃内容']) {
      expect(activeCard.getByRole(name.startsWith('查看') ? 'link' : 'button', { name })).toHaveClass('size-11')
    }
  })

  it('shows output Agents only once and keeps edit as an unfilled icon action', async () => {
    const plan: Plan = { ...mockPlans.items[0], id: 'multi-output-plan', prompt: '多种输出', agent_ids: ['wechat-article', 'seednote'] }
    vi.mocked(api.plans.list).mockResolvedValue({ items: [plan], total: 1 })
    render(<PlansPage />)

    const card = await screen.findByTestId('plan-card-multi-output-plan')
    const projectIdentity = card.querySelector('[data-slot="project-identity"]')
    expect(projectIdentity).toBeInTheDocument()
    expect(projectIdentity?.querySelector('[data-slot="agent-icon"]')).not.toBeInTheDocument()
    expect(card.querySelectorAll('[data-slot="agent-icon"]')).toHaveLength(2)

    const editButton = within(card).getByRole('button', { name: '编辑计划：多种输出' })
    expect(editButton).toHaveClass('hover:bg-muted')
    expect(editButton).not.toHaveClass('bg-primary')
    expect(editButton).not.toHaveClass('text-primary-foreground')
  })

  it.each([false, true])('shows the saved reference and submits its selection after removal=%s', async (remove) => {
    const reference = { asset_id: '44444444-4444-4444-8444-444444444444', file_name: 'cover.png', content_type: 'image/png', size: 9, download_url: 'https://signed.example/cover.png', download_expires_at: '2026-10-04T10:00:00Z' }
    const plan = { ...mockPlans.items[0], prompt: '多种输出', agent_ids: ['seednote'], reference_image: reference, skip_reference_image: false }
    vi.mocked(api.plans.list).mockResolvedValue({ items: [plan], total: 1 })
    render(<PlansPage />)
    fireEvent.click(await screen.findByRole('button', { name: '编辑计划：多种输出' }))
    const dialog = await screen.findByRole('dialog', { name: '编辑计划' })
    fireEvent.click(within(dialog).getByText('高级设置'))
    expect(within(dialog).getByRole('img', { name: '参考图' })).toHaveAttribute('src', reference.download_url)
    if (remove) fireEvent.click(within(dialog).getByRole('button', { name: '移除参考图' }))
    await waitFor(() => expect(within(dialog).getByRole('button', { name: '保存' })).toBeEnabled())
    fireEvent.click(within(dialog).getByRole('button', { name: '保存' }))
    await waitFor(() => expect(api.plans.update).toHaveBeenCalledWith(plan.id, expect.objectContaining({ reference_image: remove ? null : { asset_id: reference.asset_id } })))
  })

  it('uses a project portrait as the plan default and saves an independent portrait with its cover switch', async () => {
    const portrait = { asset_id: '55555555-5555-4555-8555-555555555555', file_name: 'project-person.png', content_type: 'image/png', size: 10, download_url: 'https://signed.example/project-person.png', download_expires_at: '2026-10-04T10:00:00Z' }
    vi.mocked(api.projects.list).mockResolvedValue([{ ...mockProjects[0], portrait_reference_image: portrait }])
    harness.upload.mockResolvedValueOnce({ uploadSessionId: '77777777-7777-4777-8777-777777777777', uploadId: 'plan-portrait', key: 'uploads/pending/plan-portrait.png', publicUrl: '', previewUrl: 'https://cdn.example/plan-portrait.png', contentType: 'image/png', size: 20 })
    render(<PlansPage />)
    const dialog = await openCreate()
    fireEvent.click(await within(dialog).findByRole('button', { name: '公众号文章' }))
    await selectProject(dialog, mockProjects[0].name)
    fireEvent.click(within(dialog).getByText('高级设置'))
    expect(within(dialog).getByRole('img', { name: '项目人物参考' })).toHaveAttribute('src', portrait.download_url)
    fireEvent.change(within(dialog).getByLabelText('人物参考图文件'), { target: { files: [new File(['portrait'], 'plan-person.png', { type: 'image/png' })] } })
    await waitFor(() => expect(harness.upload).toHaveBeenCalledWith(expect.objectContaining({ purpose: 'project_portrait_reference' })))
    fireEvent.click(within(dialog).getByRole('switch', { name: '人物封面' }))
    await waitFor(() => expect(within(dialog).getByRole('button', { name: '创建' })).toBeEnabled())
    fireEvent.click(within(dialog).getByRole('button', { name: '创建' }))
    await waitFor(() => expect(api.plans.create).toHaveBeenCalledWith(expect.objectContaining({
      portrait_reference_image: { upload_session_id: '77777777-7777-4777-8777-777777777777' },
      cover_use_portrait: true,
    })))
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
    let rejectUpload!: (error: Error) => void
    harness.upload.mockReturnValue(new Promise((_, reject) => { rejectUpload = reject }))
    fireEvent.change(within(dialog).getByLabelText('选择附件文件'), { target: { files: [new File(['x'], 'brief.txt', { type: 'text/plain' })] } })
    expect(within(dialog).getByRole('button', { name: '创建' })).toBeDisabled()
    await act(async () => rejectUpload(new Error('upload failed')))
    expect(within(dialog).getByRole('button', { name: '创建' })).toBeDisabled()
    fireEvent.click(within(dialog).getByRole('button', { name: '删除 brief.txt' }))
    await waitFor(() => expect(within(dialog).getByRole('button', { name: '创建' })).toBeEnabled())
    fireEvent.click(within(dialog).getByRole('button', { name: '创建' }))
    await waitFor(() => expect(harness.error).toHaveBeenCalled())
    expect(dialog).toBeInTheDocument()
  })
  it('submits uploaded prompt materials once and locks the pending form', async () => {
    harness.upload.mockResolvedValue({ uploadId: 'plan-upload', key: 'uploads/pending/brief.txt', contentType: 'text/plain', size: 5 })
    let finishCreate!: (value: typeof mockPlans.items[number]) => void
    vi.mocked(api.plans.create).mockReturnValue(new Promise((resolve) => { finishCreate = resolve }))
    render(<PlansPage />)
    const dialog = await openCreate()
    fireEvent.click(await within(dialog).findByRole('button', { name: '公众号文章' }))
    await selectProject(dialog, mockProjects[0].name)
    fireEvent.change(within(dialog).getByLabelText('选择附件文件'), { target: { files: [new File(['brief'], 'brief.txt', { type: 'text/plain' })] } })
    await waitFor(() => expect(within(dialog).getByRole('button', { name: '创建' })).toBeEnabled())
    fireEvent.click(within(dialog).getByRole('button', { name: '创建' }))
    await waitFor(() => expect(api.plans.create).toHaveBeenCalledTimes(1))
    expect(api.plans.create).toHaveBeenCalledWith(expect.objectContaining({ input_attachments: [expect.objectContaining({ type: 'text', upload_id: 'plan-upload', key: 'uploads/pending/brief.txt', file_name: 'brief.txt' })] }))
    expect(within(dialog).getByRole('button', { name: '取消' })).toBeDisabled()
    expect(within(dialog).getByRole('textbox', { name: '创作提示' })).toBeDisabled()
    fireEvent.submit(dialog.querySelector('form')!)
    expect(api.plans.create).toHaveBeenCalledTimes(1)
    await act(async () => finishCreate(mockPlans.items[0]))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('asks before discarding an upload even when the rest of the form is untouched', async () => {
    harness.upload.mockReturnValue(new Promise(() => {}))
    render(<PlansPage />)
    const dialog = await openCreate()
    fireEvent.change(within(dialog).getByLabelText('选择附件文件'), { target: { files: [new File(['x'], 'brief.txt', { type: 'text/plain' })] } })
    fireEvent.click(within(dialog).getByRole('button', { name: '取消' }))
    expect(await screen.findByRole('alertdialog', { name: '放弃未保存的修改？' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '继续编辑' }))
    expect(within(dialog).getByRole('button', { name: '预览 brief.txt' })).toBeInTheDocument()
  })

  it('preserves saved attachments in the composer and resets them before a new plan', async () => {
    const attachment = { type: 'text' as const, text: 'brand rules', file_name: 'rules.txt', instruction: '每次创作均参考' }
    const plan = { ...mockPlans.items[0], agent_ids: ['seednote'], input_attachments: [attachment] }
    vi.mocked(api.plans.list).mockResolvedValue({ items: [plan], total: 1 })
    render(<PlansPage />)
    fireEvent.click(await screen.findByRole('button', { name: '编辑计划：测试计划' }))
    const dialog = await screen.findByRole('dialog', { name: '编辑计划' })
    expect(within(dialog).getByRole('button', { name: '预览 rules.txt' })).toBeInTheDocument()
    await waitFor(() => expect(within(dialog).getByRole('button', { name: '保存' })).toBeEnabled())
    fireEvent.click(within(dialog).getByRole('button', { name: '保存' }))
    await waitFor(() => expect(api.plans.update).toHaveBeenCalledWith(plan.id, expect.objectContaining({ input_attachments: [attachment] })))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    const newDialog = await openCreate()
    expect(within(newDialog).queryByRole('button', { name: '预览 rules.txt' })).not.toBeInTheDocument()
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
