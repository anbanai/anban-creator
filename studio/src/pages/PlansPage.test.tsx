import { act, screen, waitFor, fireEvent, within } from '@testing-library/react'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import PlansPage from './PlansPage'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'
import type { ReferenceMaterialInputProps } from '@/components/ReferenceMaterialInput'
import type { InputAttachment, Plan, Project } from '@/types'

const { errorMock } = vi.hoisted(() => ({ errorMock: vi.fn() }))

vi.mock('sonner', () => ({ toast: { error: errorMock, success: vi.fn() } }))

const referenceMaterialInputHarness = vi.hoisted(() => ({
  props: undefined as ReferenceMaterialInputProps | undefined,
}))

vi.mock('@/components/ReferenceMaterialInput', () => ({
  ReferenceMaterialInput: (props: ReferenceMaterialInputProps) => {
    referenceMaterialInputHarness.props = props
    return <div />
  },
}))

const uploadToOSSMock = vi.hoisted(() => vi.fn())
const resolveDownloadUrlMock = vi.hoisted(() => vi.fn())

vi.mock('@/lib/direct-upload', async () => {
  const actual = await vi.importActual<typeof import('@/lib/direct-upload')>('@/lib/direct-upload')
  return { ...actual, uploadToOSS: uploadToOSSMock }
})

vi.mock('@/lib/api/uploads', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/uploads')>('@/lib/api/uploads')
  return {
    ...actual,
    uploadsApi: { ...actual.uploadsApi, resolveDownloadUrl: resolveDownloadUrlMock },
  }
})

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  const { mockPlans, mockProjects } =
    await vi.importActual<typeof import('@/test/mocks/handlers')>('@/test/mocks/handlers')
  return {
    ...actual,
    api: {
      ...actual.api,
      plans: {
        ...actual.api.plans,
        scheduleRecommendation: vi.fn().mockResolvedValue({ time: '12:07', timezone: 'Asia/Shanghai', granularity_minutes: 15, load_balanced: true }),
        list: vi.fn().mockResolvedValue(mockPlans),
        create: vi.fn(),
        update: vi.fn(),
        pause: vi.fn(),
        resume: vi.fn(),
        delete: vi.fn(),
      },
      projects: {
        ...actual.api.projects,
        list: vi.fn().mockResolvedValue(mockProjects),
      },
      billing: {
        ...actual.api.billing,
        catalog: vi.fn().mockResolvedValue({
          catalog_id: 'retail-test-v1',
          currency: 'credits',
          task_time_pricing: { timezone: 'Asia/Shanghai', peak_windows: [{ start: '09:00', end: '12:00' }, { start: '14:00', end: '18:00' }], off_peak_windows: [{ start: '00:00', end: '09:00' }, { start: '12:00', end: '14:00' }, { start: '18:00', end: '24:00' }], off_peak_rate_percent: 80, current_period: 'peak', server_time: '2026-07-31T10:00:00+08:00', next_transition_at: '2026-07-31T12:00:00+08:00' },
          skus: [
            { id: 'task.article.effective', operation: 'task.article', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 4800, peak_price_credits: 4800, off_peak_price_credits: 3840, delivery: 'article_artifacts_verified' },
            { id: 'task.article.balanced', operation: 'task.article', execution_profile: 'balanced', charge_policy: 'task_admission', price_credits: 6000, delivery: 'article_artifacts_verified' },
            { id: 'task.article.quality', operation: 'task.article', execution_profile: 'quality', charge_policy: 'task_admission', price_credits: 18000, delivery: 'article_artifacts_verified' },
            { id: 'task.seednote.effective', operation: 'task.seednote', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 4000, peak_price_credits: 4000, off_peak_price_credits: 3200, delivery: 'seednote_artifacts_verified' },
            { id: 'task.seednote.balanced', operation: 'task.seednote', execution_profile: 'balanced', charge_policy: 'task_admission', price_credits: 5000, delivery: 'seednote_artifacts_verified' },
            { id: 'task.seednote.quality', operation: 'task.seednote', execution_profile: 'quality', charge_policy: 'task_admission', price_credits: 15000, delivery: 'seednote_artifacts_verified' },
            { id: 'task.montage.effective', operation: 'task.montage', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 1600, delivery: 'montage_artifacts_verified' },
            { id: 'task.montage.balanced', operation: 'task.montage', execution_profile: 'balanced', charge_policy: 'task_admission', price_credits: 2000, delivery: 'montage_artifacts_verified' },
            { id: 'task.montage.quality', operation: 'task.montage', execution_profile: 'quality', charge_policy: 'task_admission', price_credits: 6000, delivery: 'montage_artifacts_verified' },
          ],
        }),
        wallet: vi.fn().mockResolvedValue({ paid: 0, promotional: 0, debt: 0, balance: 0 }),
      },
      agentProfiles: {
        ...actual.api.agentProfiles,
        list: vi.fn().mockResolvedValue([
          { id: 'effective', display_name: '性价比', provider: 'deepseek', model_name: 'deepseek-v4-flash', description: '适合日常创作', min_tier: 'free', available: true },
          { id: 'balanced', display_name: '平衡型', provider: 'volcengine_ark', model_name: 'doubao-seed-evolving', description: '质量与速度平衡', min_tier: 'pro', available: true },
          { id: 'quality', display_name: '极致效果', provider: 'moonshot', model_name: 'kimi-k3[1m]', description: '复杂高质量创作', min_tier: 'enterprise', available: true },
        ]),
      },
      imageCapabilities: {
        ...actual.api.imageCapabilities,
        list: vi.fn().mockResolvedValue({
          tier: 'pro',
          default_capability: 'standard',
          items: [{ key: 'standard', display_name: '标准图像', price_available: true, enabled: true }],
        }),
      },
    },
  }
})

describe('PlansPage — mutation failure feedback (no silent failure)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    uploadToOSSMock.mockImplementation(async ({ file }: { file: File }) => ({
      uploadId: `upload-${file.name}`,
      key: `uploads/pending/user/${file.name}`,
      publicUrl: `https://cdn.example/${file.name}?signed=secret`,
      contentType: file.type,
      size: file.size,
    }))
    window.history.pushState({}, '', '/')
    vi.mocked(api.plans.list).mockResolvedValue({
      items: [{
        id: 'plan-1',
        type: 'article',
        title: '测试计划',
        description: '',
        cron_expr: '0 9 * * 1',
        prompt: '',
        status: 'active',
        next_run_at: '2025-01-20T09:00:00Z',
        project_id: 'ch-1',
        execution_profile: 'quality',
        created_at: '2025-01-10T00:00:00Z',
        updated_at: '2025-01-10T00:00:00Z',
      }],
      total: 1,
    })
    vi.mocked(api.projects.list).mockResolvedValue([{
      id: 'ch-1',
      user_id: '1',
      platform: 'article',
      name: '测试项目',
      avatar_url: '',
      profile_url: 'https://mp.weixin.qq.com/test',
      instructions: '测试定位',
      keywords: '测试',
      visual_style: '',
      writer: '',
      theme: '',
      author: '作者',
      template_id: '',
      image_ratio: '16:9',
      max_concurrent_tasks: 2,
      config: { wechat_app_id: 'wx123' },
      status: 'active',
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    }])
    vi.mocked(api.billing.wallet).mockResolvedValue({ paid: 0, promotional: 0, debt: 0, balance: 0 })
  })

  it('creates a plan with the selected server-backed execution profile and exact price', async () => {
    vi.mocked(api.billing.wallet).mockResolvedValueOnce({ paid: 10000, promotional: 0, debt: 0, balance: 10000 })
    window.history.pushState({}, '', '/plans?create=true&type=article&project_id=ch-1&intent=schedule')
    render(<PlansPage />)

    const dialog = await screen.findByRole('dialog', { name: '新建计划' })
    expect(await within(dialog).findByRole('button', { name: /^性价比，/ })).toHaveAttribute('aria-pressed', 'true')
    expect(within(dialog).getByText('4,800 积分')).toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: /^平衡型，/ }))
    fireEvent.click(within(dialog).getByRole('button', { name: '创建' }))

    await waitFor(() => expect(api.plans.create).toHaveBeenCalledWith(expect.objectContaining({
      execution_profile: 'balanced',
    })))
  })

  it('applies the backend off-peak recommendation only to a new plan', async () => {
    render(<PlansPage />)
    fireEvent.click(await screen.findByRole('button', { name: '新建计划' }))
    const createDialog = await screen.findByRole('dialog', { name: '新建计划' })
    expect(await within(createDialog).findByText(/12:07 自动执行/)).toBeInTheDocument()
    expect(within(createDialog).getByText('当前选择为低峰时段 · 预计 3200 积分/次')).toBeInTheDocument()
    expect(within(createDialog).getByText('高峰 4000 积分 · 低峰 3200 积分 · 低峰可节省 800 积分')).toBeInTheDocument()
    expect(api.plans.scheduleRecommendation).toHaveBeenCalledTimes(1)
  })

  it('does not request or overwrite a recommendation when editing', async () => {
    render(<PlansPage />)
    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    const editDialog = await screen.findByRole('dialog', { name: '编辑计划' })
    expect(within(editDialog).getByText(/09:00 自动执行/)).toBeInTheDocument()
    expect(api.plans.scheduleRecommendation).not.toHaveBeenCalled()
  })

  it('uses a catalog-derived off-peak fallback when recommendation is unavailable', async () => {
    vi.mocked(api.plans.scheduleRecommendation).mockRejectedValueOnce(new Error('unavailable'))
    render(<PlansPage />)
    fireEvent.click(await screen.findByRole('button', { name: '新建计划' }))
    const dialog = await screen.findByRole('dialog', { name: '新建计划' })
    expect(await within(dialog).findByText(/00:00 自动执行/)).toBeInTheDocument()
    expect(within(dialog).getByText('智能分布暂不可用，已使用当前配置中的低峰时间。')).toBeInTheDocument()
  })

  it('retains the stored execution profile when editing a plan', async () => {
    render(<PlansPage />)
    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))

    const dialog = await screen.findByRole('dialog', { name: '编辑计划' })
    expect(await within(dialog).findByRole('button', { name: /^极致效果，/ })).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(within(dialog).getByRole('button', { name: '更新' }))

    await waitFor(() => expect(api.plans.update).toHaveBeenCalledWith('plan-1', expect.objectContaining({
      execution_profile: 'quality',
    })))
  })

  it('blocks plan updates when the stored execution profile is no longer available', async () => {
    vi.mocked(api.agentProfiles.list).mockResolvedValueOnce([
      { id: 'effective', display_name: '性价比', provider: 'deepseek', model_name: 'deepseek-v4-flash', description: '适合日常创作', min_tier: 'free', available: true },
      { id: 'balanced', display_name: '平衡型', provider: 'volcengine_ark', model_name: 'doubao-seed-evolving', description: '质量与速度平衡', min_tier: 'pro', available: true },
      { id: 'quality', display_name: '极致效果', provider: 'moonshot', model_name: 'kimi-k3[1m]', description: '复杂高质量创作', min_tier: 'enterprise', available: false, unavailable_reason: 'requires_enterprise' },
    ])
    render(<PlansPage />)
    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))

    const dialog = await screen.findByRole('dialog', { name: '编辑计划' })
    expect(await within(dialog).findByRole('button', { name: '更新' })).toBeDisabled()
    fireEvent.click(within(dialog).getByRole('button', { name: '更新' }))
    expect(api.plans.update).not.toHaveBeenCalled()
  })

  it('renders the shared composer with project context in the create dialog', async () => {
    render(<PlansPage />)
    fireEvent.click(await screen.findByRole('button', { name: '新建计划' }))
    await screen.findByRole('dialog')
    expect(document.querySelector('[data-slot="agent-prompt-input"]')).toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: '项目上下文' })).toBeInTheDocument()
    expect(document.querySelectorAll('form form')).toHaveLength(0)
  })

  it('shows an error toast when pausing a plan fails (was previously silent)', async () => {
    // pause is wired through useSubmitLock().submit(mutateAsync) with no catch —
    // before this fix, a rejection surfaced nothing to the user. Now the server's
    // reason (response.data.msg) is surfaced via getApiErrorMessage.
    vi.mocked(api.plans.pause).mockRejectedValueOnce({
      response: { data: { msg: '排期冲突，请检查现有计划' } },
    })

    render(<PlansPage />)

    // mockPlans[0] is an active plan → the 暂停 button renders.
    const pauseBtn = await screen.findByRole('button', { name: '暂停' }, { timeout: 5000 })
    fireEvent.click(pauseBtn)

    await waitFor(() => {
      expect(errorMock).toHaveBeenCalledWith('排期冲突，请检查现有计划')
    })
  })

  it('does not offer e-commerce as a plan content type', async () => {
    render(<PlansPage />)

    fireEvent.click(await screen.findByRole('button', { name: '新建计划' }))
    const dialog = await screen.findByRole('dialog', { name: '新建计划' })
    const [typeSelect] = within(dialog).getAllByRole('combobox')
    fireEvent.click(typeSelect)

    expect(await screen.findByRole('option', { name: '公众号文章' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: '种草笔记' })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: '电商出图' })).not.toBeInTheDocument()
  })

  it('highlights a plan addressed by the timeline highlight parameter', async () => {
    window.history.pushState({}, '', '/plans?highlight=plan-1')

    render(<PlansPage />)

    const highlightedPlan = await screen.findByTestId('plan-card-plan-1')
    expect(highlightedPlan).toHaveAttribute('data-highlighted', 'true')
  })

  it('opens create dialog from URL intent with the project context preselected', async () => {
    window.history.pushState({}, '', '/plans?create=true&type=article&project_id=ch-1&intent=schedule')

    render(<PlansPage />)

    const dialog = await screen.findByRole('dialog', { name: '新建计划' })
    expect(dialog).toBeInTheDocument()
    expect(within(dialog).getByText('测试项目')).toBeInTheDocument()
  })

  it('uses the cheapest available profile price for article plan runs', async () => {
    vi.mocked(api.billing.wallet).mockResolvedValueOnce({ paid: 7000, promotional: 0, debt: 0, balance: 7000 })
    window.history.pushState({}, '', '/plans?create=true&type=article&project_id=ch-1&intent=schedule')

    render(<PlansPage />)

    const dialog = await screen.findByRole('dialog', { name: '新建计划' })
    const price = await within(dialog).findByText('4,800 积分')
    expect(price.parentElement).toHaveTextContent('当前每次执行固定价：4,800 积分')
    expect(within(dialog).getByText(/余额：7,000 →/)).toBeInTheDocument()
    expect(within(dialog).getByText('2,200')).toBeInTheDocument()
    expect(within(dialog).queryByText(/运行预留/)).not.toBeInTheDocument()
    expect(within(dialog).queryByText(/积分不足/)).not.toBeInTheDocument()
  })
})

describe('PlansPage image capability contract', () => {
  it('rejects a retired image capability before submission', () => {
    const source = readFileSync(resolve(import.meta.dirname, 'PlansPage.tsx'), 'utf8')
    expect(source).toContain('该图像能力已停用，请重新选择')
    expect(source).toContain('submittedImageCapability.price_available !== true')
    expect(source).toContain('submittedImageCapability.enabled !== true')
  })

  it('describes watermark support without exposing an image provider', () => {
    const source = readFileSync(resolve(import.meta.dirname, 'PlansPage.tsx'), 'utf8')
    expect(source).toContain('仅在所选图像能力支持水印时生效')
    expect(source).not.toContain('火山引擎')
  })

  it('persists image ratio and constrains it with the effective capability', () => {
    const source = readFileSync(resolve(import.meta.dirname, 'PlansPage.tsx'), 'utf8')
    expect(source).toContain('image_ratio: normalizeImageRatio(plan.image_ratio)')
    expect(source).toContain('image_ratio: values.image_ratio')
    expect(source).toContain('supportedSizes={selectedImageCapability?.features?.size_presets}')
    expect(source).toContain('normalizeImageRatio(fullProject?.image_ratio)')
    expect(source).toContain('imageRatioUnsupported')
    expect(source).toContain('当前图像能力不支持所选比例，请重新选择比例或智能适配')
  })
})


describe('PlansPage Seednote reference snapshots', () => {
  const savedReferenceAttachment: InputAttachment = {
    type: 'image',
    asset_id: '22222222-2222-4222-8222-222222222222',
    file_name: 'plan-reference.png',
    content_type: 'image/png',
    size: 9,
  }
  const savedAttachment: InputAttachment = {
    type: 'image',
    url: '/saved-product.png',
    file_name: 'saved-product.png',
    content_type: 'image/png',
    upload_id: 'upload-saved',
    key: 'uploads/saved-product.png',
    instruction: '保留包装、Logo 和瓶身比例',
  }
  const seednoteProject = {
    id: 'seednote-project-1',
    user_id: '1',
    platform: 'seednote',
    name: '种草项目',
    avatar_url: '',
    profile_url: '',
    instructions: '面向敏感肌用户',
    keywords: '护肤',
    visual_style: '',
    writer: '',
    theme: '',
    author: '',
    template_id: '',
    image_ratio: '3:4',
    max_concurrent_tasks: 2,
    config: {},
    status: 'active',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  } as Project
  const seednotePlan = {
    id: 'seednote-plan-1',
    type: 'seednote',
    title: '每日种草计划',
    description: '',
    cron_expr: '0 9 * * 1,3,5',
    prompt: '围绕敏感肌保湿创作',
    status: 'active',
    next_run_at: '2025-01-20T09:00:00Z',
    project_id: seednoteProject.id,
    execution_profile: 'effective',
    input_attachments: [savedReferenceAttachment, savedAttachment],
    created_at: '2025-01-10T00:00:00Z',
    updated_at: '2025-01-10T00:00:00Z',
  } as Plan

  beforeEach(() => {
    vi.clearAllMocks()
    window.history.pushState({}, '', '/')
    uploadToOSSMock.mockImplementation(async ({ file }: { file: File }) => ({
      uploadId: `upload-${file.name}`,
      key: `uploads/pending/user/${file.name}`,
      publicUrl: `https://cdn.example/${file.name}?signed=secret`,
      contentType: file.type,
      size: file.size,
    }))
    vi.mocked(api.projects.list).mockResolvedValue([seednoteProject])
    vi.mocked(api.plans.list).mockResolvedValue({ items: [seednotePlan], total: 1 })
    vi.mocked(api.plans.create).mockResolvedValue(seednotePlan)
    vi.mocked(api.plans.update).mockResolvedValue(seednotePlan)
    vi.mocked(api.billing.wallet).mockResolvedValue({ paid: 10000, promotional: 0, debt: 0, balance: 10000 })
    resolveDownloadUrlMock.mockResolvedValue({
      url: 'https://cdn.example.com/signed-plan-attachment.png',
      expires_at: '2026-07-17T12:00:00Z',
    })
  })

  it('creates a Seednote plan with the current reference snapshot', async () => {
    window.history.pushState({}, '', `/plans?create=true&type=seednote&project_id=${seednoteProject.id}&intent=schedule`)
    render(<PlansPage />)

    const dialog = await screen.findByRole('dialog', { name: '新建计划' })
    expect(within(dialog).queryByRole('button', { name: '创建计划' })).not.toBeInTheDocument()
    expect(within(dialog).getAllByRole('button', { name: '创建' })).toHaveLength(1)
    const file = new File(['saved'], 'saved-product.png', { type: 'image/png' })
    fireEvent.change(screen.getByLabelText('选择附件文件'), { target: { files: [file] } })
    await screen.findByRole('button', { name: '预览 saved-product.png' })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() => {
      expect(api.plans.create).toHaveBeenCalledWith(expect.objectContaining({
        type: 'seednote',
        input_attachments: [{
          type: 'image',
          upload_id: 'upload-saved-product.png',
          key: 'uploads/pending/user/saved-product.png',
          file_name: 'saved-product.png',
          content_type: 'image/png',
          size: file.size,
        }],
      }))
    })
  })

  it('omits untouched ordered plan materials from the update payload', async () => {
    render(<PlansPage />)

    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    expect(await screen.findByRole('button', { name: '预览 plan-reference.png' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '更新' }))

    await waitFor(() => expect(api.plans.update).toHaveBeenCalled())
    const payload = vi.mocked(api.plans.update).mock.calls[0][1]
    expect(payload).not.toHaveProperty('input_attachments')
    expect(payload).not.toHaveProperty('reference_image')
    expect(payload).not.toHaveProperty('reference_image_url')
  })

  it('removes an ordered plan image only when it is explicitly deleted', async () => {
    render(<PlansPage />)

    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    fireEvent.click(await screen.findByRole('button', { name: '删除 plan-reference.png' }))
    fireEvent.click(screen.getByRole('button', { name: '更新' }))

    await waitFor(() => expect(api.plans.update).toHaveBeenCalled())
    const payload = vi.mocked(api.plans.update).mock.calls[0][1]
    expect(payload.input_attachments).toEqual([expect.objectContaining({
      type: 'image',
      upload_id: 'upload-saved',
      key: 'uploads/saved-product.png',
      file_name: 'saved-product.png',
    })])
    expect(payload).not.toHaveProperty('reference_image')
    expect(payload).not.toHaveProperty('reference_image_url')
  })

  it('replaces an ordered plan image with an uploaded attachment', async () => {
    uploadToOSSMock.mockResolvedValueOnce({
      uploadSessionId: '55555555-5555-4555-8555-555555555555',
      uploadId: 'upload-plan-reference',
      key: 'uploads/pending/plan-reference.png',
      publicUrl: 'https://staging.example/plan-reference.png',
      previewUrl: 'https://staging.example/plan-reference.png',
      contentType: 'image/png',
      size: 9,
    })
    render(<PlansPage />)

    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    fireEvent.click(await screen.findByRole('button', { name: '删除 plan-reference.png' }))
    fireEvent.change(screen.getByLabelText('选择附件文件'), {
      target: { files: [new File(['replacement'], 'plan-replacement.png', { type: 'image/png' })] },
    })
    await waitFor(() => expect(screen.getByRole('button', { name: '更新' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '更新' }))

    await waitFor(() => expect(api.plans.update).toHaveBeenCalled())
    const payload = vi.mocked(api.plans.update).mock.calls[0][1]
    expect(payload.input_attachments).toEqual([
      expect.objectContaining({ upload_id: 'upload-saved', key: 'uploads/saved-product.png' }),
      expect.objectContaining({
        type: 'image',
        upload_id: 'upload-plan-reference',
        key: 'uploads/pending/plan-reference.png',
        file_name: 'plan-replacement.png',
      }),
    ])
    expect(payload).not.toHaveProperty('reference_image')
    expect(payload).not.toHaveProperty('reference_image_url')
  })

  it('keeps a replacement attachment open when plan update fails', async () => {
    uploadToOSSMock.mockResolvedValueOnce({
      uploadSessionId: '66666666-6666-4666-8666-666666666666',
      uploadId: 'upload-expired-plan-reference',
      key: 'uploads/pending/expired-plan-reference.png',
      publicUrl: 'https://staging.example/expired-plan-reference.png',
      previewUrl: 'https://staging.example/expired-plan-reference.png',
      contentType: 'image/png',
      size: 9,
    })
    vi.mocked(api.plans.update).mockRejectedValueOnce({
      response: { data: { msg: '参考图上传会话已过期，请重新上传' } },
    })
    render(<PlansPage />)

    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    fireEvent.click(await screen.findByRole('button', { name: '删除 plan-reference.png' }))
    fireEvent.change(screen.getByLabelText('选择附件文件'), {
      target: { files: [new File(['replacement'], 'expired.png', { type: 'image/png' })] },
    })
    const preview = await screen.findByRole('button', { name: '预览 expired.png' })
    await waitFor(() => expect(screen.getByRole('button', { name: '更新' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '更新' }))

    await waitFor(() => expect(errorMock).toHaveBeenCalledWith('参考图上传会话已过期，请重新上传'))
    expect(screen.getByRole('dialog', { name: '编辑计划' })).toBeInTheDocument()
    expect(preview).toBeInTheDocument()
    const payload = vi.mocked(api.plans.update).mock.calls[0][1]
    expect(payload.input_attachments).toEqual([
      expect.objectContaining({ upload_id: 'upload-saved', key: 'uploads/saved-product.png' }),
      expect.objectContaining({
        type: 'image',
        upload_id: 'upload-expired-plan-reference',
        key: 'uploads/pending/expired-plan-reference.png',
        file_name: 'expired.png',
      }),
    ])
    expect(payload).not.toHaveProperty('reference_image')
    expect(payload).not.toHaveProperty('reference_image_url')
  })

  it('hydrates edit snapshots without marking ordered materials as changed', async () => {
    render(<PlansPage />)

    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    expect(await screen.findByRole('dialog', { name: '编辑计划' })).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: '预览 saved-product.png' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '更新' }))
    await waitFor(() => expect(api.plans.update).toHaveBeenCalledTimes(1))
    expect(vi.mocked(api.plans.update).mock.calls[0][1]).not.toHaveProperty('input_attachments')
  })

  it('guards native form submit until plan reference uploads finish', async () => {
    window.history.pushState({}, '', `/plans?create=true&type=seednote&project_id=${seednoteProject.id}&intent=schedule`)
    let resolveUpload!: (value: unknown) => void
    uploadToOSSMock.mockImplementationOnce(() => new Promise((resolve) => { resolveUpload = resolve }))
    render(<PlansPage />)

    expect(await screen.findByRole('dialog', { name: '新建计划' })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('选择附件文件'), {
      target: { files: [new File(['pending'], 'pending.png', { type: 'image/png' })] },
    })

    expect(screen.getByRole('button', { name: '创建' })).toBeDisabled()
    const form = document.getElementById('plan-form')
    expect(form).toBeInstanceOf(HTMLFormElement)
    fireEvent.submit(form!)
    await act(async () => { await Promise.resolve() })
    expect(api.plans.create).not.toHaveBeenCalled()

    await act(async () => {
      resolveUpload({ uploadId: 'pending', key: 'uploads/pending/pending.png', publicUrl: '', contentType: 'image/png', size: 7 })
      await Promise.resolve()
    })

    await waitFor(() => expect(screen.getByRole('button', { name: '创建' })).toBeEnabled())
    fireEvent.submit(form!)
    await waitFor(() => expect(api.plans.create).toHaveBeenCalledWith(expect.objectContaining({
      input_attachments: [expect.objectContaining({
        upload_id: 'pending',
        key: 'uploads/pending/pending.png',
      })],
    })))
  })

  it('does not dirty hydrated attachments but marks instruction edits dirty', async () => {
    render(<PlansPage />)
    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    await screen.findByRole('button', { name: '预览 saved-product.png' })
    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '编辑计划' })).not.toBeInTheDocument())
    expect(screen.queryByRole('alertdialog', { name: '放弃编辑？' })).not.toBeInTheDocument()

    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    await screen.findByRole('button', { name: '预览 saved-product.png' })
    fireEvent.click(screen.getByRole('button', { name: '编辑 saved-product.png 的附件说明' }))
    fireEvent.change(await screen.findByRole('textbox', { name: '附件说明' }), {
      target: { value: '改用新版包装说明' },
    })
    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    expect(await screen.findByRole('alertdialog', { name: '放弃编辑？' })).toBeInTheDocument()
  })

  it('keeps legacy owner keys for preview but normalizes touched snapshots before update', async () => {
    const legacyPlan = {
      ...seednotePlan,
      input_attachments: [
        {
          type: 'image',
          url: '/api/v1/files/plans/seednote-plan-1/input/legacy.png',
          key: 'plans/seednote-plan-1/input/legacy.png',
          file_name: 'legacy.png',
          content_type: 'image/png',
          size: 20,
          instruction: '保留原图',
        },
        {
          type: 'document',
          key: 'plans/seednote-plan-1/input/key-only.pdf',
          file_name: 'key-only.pdf',
          content_type: 'application/pdf',
          size: 30,
        },
      ],
    } as Plan
    vi.mocked(api.plans.list).mockResolvedValue({ items: [legacyPlan], total: 1 })
    vi.mocked(api.plans.update).mockResolvedValue(legacyPlan)
    render(<PlansPage />)

    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    const dialog = await screen.findByRole('dialog', { name: '编辑计划' })
    fireEvent.click(within(dialog).getByRole('button', { name: '预览 legacy.png' }))
    await waitFor(() => expect(resolveDownloadUrlMock).toHaveBeenCalledWith({
      key: 'plans/seednote-plan-1/input/legacy.png',
      owner_type: 'plan',
      owner_id: 'seednote-plan-1',
    }))
    fireEvent.click(screen.getByRole('button', { name: '关闭附件预览' }))

    fireEvent.click(within(dialog).getByRole('button', { name: '更新' }))
    await waitFor(() => expect(api.plans.update).toHaveBeenCalledTimes(1))
    expect(vi.mocked(api.plans.update).mock.calls[0][1]).not.toHaveProperty('input_attachments')
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '编辑计划' })).not.toBeInTheDocument())

    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    const editedDialog = await screen.findByRole('dialog', { name: '编辑计划' })
    fireEvent.click(within(editedDialog).getByRole('button', { name: '编辑 legacy.png 的附件说明' }))
    fireEvent.change(await screen.findByRole('textbox', { name: '附件说明' }), {
      target: { value: '使用新版说明' },
    })
    fireEvent.click(within(editedDialog).getByRole('button', { name: '更新' }))
    expect(await within(editedDialog).findByText('附件 key-only.pdf 缺少可复用的内部文件地址，请删除后重新上传')).toBeInTheDocument()
    expect(api.plans.update).toHaveBeenCalledTimes(1)

    fireEvent.click(within(editedDialog).getByRole('button', { name: '删除 key-only.pdf' }))
    fireEvent.click(within(editedDialog).getByRole('button', { name: '更新' }))
    await waitFor(() => expect(api.plans.update).toHaveBeenCalledTimes(2))
    expect(vi.mocked(api.plans.update).mock.calls[1][1]).toEqual(expect.objectContaining({
      input_attachments: [{
        type: 'image',
        url: '/api/v1/files/plans/seednote-plan-1/input/legacy.png',
        file_name: 'legacy.png',
        content_type: 'image/png',
        size: 20,
        instruction: '使用新版说明',
      }],
    }))
  })
})

describe('PlansPage Montage input', () => {
  const montageProject = {
    id: 'project-montage',
    user_id: '1',
    platform: 'montage',
    name: 'Montage 项目',
    avatar_url: '',
    profile_url: '',
    instructions: '新品短视频',
    keywords: '',
    visual_style: '',
    writer: '',
    theme: '',
    author: '',
    template_id: '',
    reference_image_url: '',
    image_ratio: '16:9',
    montage_defaults: {
      default_pipeline: 'project-pipeline',
      preferences: {
        aspect_ratio: '16:9',
        duration_seconds: 45,
        style: 'project style',
        music_prompt: 'project music',
        subtitle_mode: 'burned-in',
        voiceover_mode: 'narrated',
      },
      asset_guidance: '优先使用实拍素材',
      delivery_targets: ['final_video'],
    },
    max_concurrent_tasks: 2,
    config: {},
    status: 'active',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  } as Project
  const savedMontagePlan = {
    id: 'montage-plan-1',
    type: 'montage',
    title: 'Montage 周计划',
    description: '',
    cron_expr: '0 9 * * 1',
    prompt: '',
    status: 'active',
    next_run_at: '2025-01-20T09:00:00Z',
    project_id: montageProject.id,
    execution_profile: 'effective',
    montage_input: {
      brief: '保存的 brief',
      pipeline_key: 'saved-pipeline',
      source_assets: [],
      preferences: { aspect_ratio: '9:16', duration_seconds: 12 },
      delivery_targets: [],
    },
    created_at: '2025-01-10T00:00:00Z',
    updated_at: '2025-01-10T00:00:00Z',
  } as Plan

  beforeEach(() => {
    vi.clearAllMocks()
    window.history.pushState({}, '', '/')
    referenceMaterialInputHarness.props = undefined
    vi.mocked(api.projects.list).mockResolvedValue([montageProject])
    vi.mocked(api.plans.list).mockResolvedValue({ items: [], total: 0 })
    vi.mocked(api.plans.create).mockResolvedValue(savedMontagePlan)
    vi.mocked(api.plans.update).mockResolvedValue(savedMontagePlan)
    vi.mocked(api.billing.wallet).mockResolvedValue({ paid: 10000, promotional: 0, debt: 0, balance: 10000 })
  })

  it('inherits project defaults and creates a plan with complete Montage input', async () => {
    window.history.pushState({}, '', `/plans?create=true&type=montage&project_id=${montageProject.id}&intent=schedule`)
    render(<PlansPage />)

    expect(await screen.findByRole('dialog', { name: '新建计划' })).toBeInTheDocument()
    expect(await screen.findByDisplayValue('project-pipeline')).toBeInTheDocument()
    expect(screen.getByLabelText('时长（秒）')).toHaveValue(45)
    await waitFor(() => expect(referenceMaterialInputHarness.props?.uploadPurpose).toBe('montage_asset'))

    fireEvent.change(screen.getByPlaceholderText('描述每次计划的创作方向、内容要求和素材使用方式...'), {
      target: { value: '每周新品发布短片' },
    })
    act(() => {
      referenceMaterialInputHarness.props?.onChange([{
        type: 'video',
        url: '/source.mp4',
        file_name: 'source.mp4',
        content_type: 'video/mp4',
        size: 42,
      }])
    })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() => expect(api.plans.create).toHaveBeenCalledWith(expect.objectContaining({
      type: 'montage',
      project_id: montageProject.id,
      montage_input: expect.objectContaining({
        brief: '每周新品发布短片',
        pipeline_key: 'project-pipeline',
        source_assets: [expect.objectContaining({ type: 'video_url', url: '/source.mp4' })],
        preferences: {
          aspect_ratio: '16:9',
          duration_seconds: 45,
          style: 'project style',
          music_prompt: 'project music',
          subtitle_mode: 'burned-in',
          voiceover_mode: 'narrated',
        },
        delivery_targets: ['final_video'],
      }),
    })))
  })

  it('keeps saved Montage input ahead of current project defaults when editing', async () => {
    vi.mocked(api.plans.list).mockResolvedValue({ items: [savedMontagePlan], total: 1 })
    render(<PlansPage />)

    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    expect(await screen.findByRole('dialog', { name: '编辑计划' })).toBeInTheDocument()
    expect(screen.getByDisplayValue('saved-pipeline')).toBeInTheDocument()
    expect(screen.getByLabelText('时长（秒）')).toHaveValue(12)
    expect(screen.getByDisplayValue('9:16')).toBeInTheDocument()
    expect(screen.queryByText('final_video')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '更新' }))

    await waitFor(() => expect(api.plans.update).toHaveBeenCalledWith(
      savedMontagePlan.id,
      expect.objectContaining({
        montage_input: expect.objectContaining({
          pipeline_key: 'saved-pipeline',
          preferences: expect.objectContaining({ duration_seconds: 12, aspect_ratio: '9:16' }),
          delivery_targets: [],
        }),
      }),
    ))
  })

  it('blocks Montage plan save while source assets are uploading', async () => {
    window.history.pushState({}, '', `/plans?create=true&type=montage&project_id=${montageProject.id}&intent=schedule`)
    render(<PlansPage />)

    expect(await screen.findByRole('dialog', { name: '新建计划' })).toBeInTheDocument()
    await waitFor(() => expect(referenceMaterialInputHarness.props?.uploadPurpose).toBe('montage_asset'))

    act(() => {
      referenceMaterialInputHarness.props?.onUploadingChange?.(true)
    })

    expect(screen.getByRole('button', { name: '创建' })).toBeDisabled()
  })
})
