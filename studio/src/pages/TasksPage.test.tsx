import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

import TasksPage from './TasksPage'
import { api } from '@/lib/api'
import { createTestQueryClient } from '@/test/test-utils'
import type { ReferenceMaterialInputProps } from '@/components/ReferenceMaterialInput'
import type { Project, Task } from '@/types'
import { AgentPromptDropProvider } from '@/components/agent-prompt/AgentPromptDropProvider'

const { errorMock } = vi.hoisted(() => ({ errorMock: vi.fn() }))

vi.mock('sonner', () => ({ toast: { error: errorMock, message: vi.fn(), success: vi.fn() } }))

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

vi.mock('@/lib/direct-upload', async () => {
  const actual = await vi.importActual<typeof import('@/lib/direct-upload')>('@/lib/direct-upload')
  return { ...actual, uploadToOSS: uploadToOSSMock }
})

const fixtures = vi.hoisted(() => {
  const project = {
    id: 'project-1',
    user_id: 'user-1',
    platform: 'article',
    name: '公众号项目',
    avatar_url: '',
    profile_url: '',
    keywords: '',
    visual_style: '',
    writer: '',
    theme: '',
    author: '',
    template_id: '',
    image_ratio: '16:9',
    max_concurrent_tasks: 1,
    config: {},
    status: 'active',
    created_at: '2026-07-01T00:00:00.000Z',
    updated_at: '2026-07-01T00:00:00.000Z',
  }
  const failedTask = {
    id: 'failed-task',
    type: 'article',
    title: '失败文章',
    prompt: '失败任务',
    status: 'failed',
    progress: 0,
    error_message: '模型超时',
    plan_id: null,
    project_id: project.id,
    execution_profile: 'effective',
    result: null,
    published: false,
    published_at: null,
    billing_price_credits: 6000,
    created_at: '2026-07-06T01:00:00.000Z',
    started_at: '',
    completed_at: '',
  }
  const approvalTask = {
    id: 'approval-task',
    type: 'article',
    title: '待确认草稿',
    prompt: '审批任务',
    status: 'completed',
    progress: 100,
    plan_id: null,
    project_id: project.id,
    execution_profile: 'effective',
    result: null,
    published: false,
    published_at: null,
    billing_price_credits: 6000,
    publish_approval_state: 'pending',
    workflow_status: {
      version: 'creation_workflow_v1',
      current_stage: 'review',
      stages: [{ key: 'review', label: '质量复盘', status: 'completed' }],
      review: { overall_score: 88, readiness: 'ready', risks: [], next_actions: [], strengths: [] },
    },
    created_at: '2026-07-06T02:00:00.000Z',
    started_at: '',
    completed_at: '2026-07-06T02:10:00.000Z',
  }
  return { project, failedTask, approvalTask }
})

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      tasks: {
        ...actual.api.tasks,
        list: vi.fn().mockResolvedValue({ items: [fixtures.failedTask], total: 1 }),
        create: vi.fn(),
        bulkCancel: vi.fn(),
        bulkClone: vi.fn(),
        bulkDelete: vi.fn(),
        downloadBulkZipBlob: vi.fn(),
        markPublished: vi.fn(),
      },
      projects: {
        ...actual.api.projects,
        list: vi.fn().mockResolvedValue([fixtures.project]),
        platformConfigs: vi.fn().mockResolvedValue([]),
      },
      billing: {
        ...actual.api.billing,
        wallet: vi.fn().mockResolvedValue({ paid: 1000, promotional: 0, debt: 0, balance: 1000 }),
        catalog: vi.fn().mockResolvedValue({
          catalog_id: 'retail-test-v1',
          currency: 'credits',
          skus: [
            { id: 'task.article.effective', operation: 'task.article', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 6000, delivery: 'article_artifacts_verified' },
            { id: 'task.seednote.effective', operation: 'task.seednote', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 5000, delivery: 'seednote_artifacts_verified' },
            { id: 'task.ecommerce.effective', operation: 'task.ecommerce', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 3000, delivery: 'ecommerce_artifacts_verified' },
            { id: 'task.montage.effective', operation: 'task.montage', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 2000, delivery: 'montage_artifacts_verified' },
          ],
        }),
      },
      agentProfiles: {
        ...actual.api.agentProfiles,
        list: vi.fn().mockResolvedValue([
          { id: 'effective', display_name: '性价比', provider: 'deepseek', model_name: 'deepseek-v4-flash', description: '适合日常创作', min_tier: 'free', available: true },
          { id: 'balanced', display_name: '平衡型', provider: 'volcengine_ark', model_name: 'doubao-seed-evolving', description: '质量与速度平衡', min_tier: 'pro', available: true },
          { id: 'quality', display_name: '极致效果', provider: 'moonshot', model_name: 'kimi-k3[1m]', description: '复杂高质量创作', min_tier: 'enterprise', available: true },
        ]),
      },
    },
  }
})

function renderTasksPage(initialPath = '/tasks') {
  const queryClient = createTestQueryClient()
  return {
    queryClient,
    ...render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={[initialPath]}>
          <AgentPromptDropProvider>
            <Routes>
              <Route path="/tasks" element={<TasksPage />} />
              <Route path="/tasks/:id" element={<div>created task detail</div>} />
            </Routes>
          </AgentPromptDropProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  }
}

describe('TasksPage unified prompt composer', () => {
  it('renders the shared composer with project context in the create dialog', async () => {
    renderTasksPage()
    fireEvent.click(await screen.findByRole('button', { name: '新建任务' }))
    const dialog = await screen.findByRole('dialog')
    expect(document.querySelector('[data-slot="agent-prompt-input"]')).toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: '项目上下文' })).toBeInTheDocument()
    expect(within(dialog).queryByRole('button', { name: '创建任务' })).not.toBeInTheDocument()
    expect(within(dialog).getAllByRole('button', { name: '创建' })).toHaveLength(1)
    expect(document.querySelectorAll('form form')).toHaveLength(0)
  })

  it('uses the composer prompt as the Montage brief', async () => {
    const montageProject = { ...fixtures.project, id: 'montage-project', platform: 'montage', name: '剪辑项目' } as Project
    vi.mocked(api.projects.list).mockResolvedValue([montageProject])
    vi.mocked(api.billing.wallet).mockResolvedValue({ paid: 100000, promotional: 0, debt: 0, balance: 100000 })
    vi.mocked(api.tasks.create).mockResolvedValue({ ...fixtures.failedTask, id: 'montage-task', type: 'montage', project_id: montageProject.id } as Task)
    renderTasksPage(`/tasks?create=true&type=montage&project_id=${montageProject.id}&intent=new`)

    await screen.findByRole('dialog', { name: '新建任务' })
    fireEvent.change(screen.getByPlaceholderText('描述创作目标、内容要求和素材使用方式...'), {
      target: { value: '将访谈素材剪成 30 秒竖版短片' },
    })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() => expect(api.tasks.create).toHaveBeenCalledWith(expect.objectContaining({
      prompt: '将访谈素材剪成 30 秒竖版短片',
      montage_input: expect.objectContaining({ brief: '将访谈素材剪成 30 秒竖版短片' }),
      input_attachments: [],
    })))
  })

  it('navigates to the created task after shared dialog submission', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([fixtures.project as Project])
    vi.mocked(api.billing.wallet).mockResolvedValue({ paid: 10000, promotional: 0, debt: 0, balance: 10000 })
    vi.mocked(api.tasks.create).mockResolvedValue({
      ...fixtures.failedTask,
      id: 'created-task',
      status: 'pending',
    } as Task)
    renderTasksPage('/tasks?create=true&type=article&project_id=project-1&intent=new')

    await screen.findByRole('dialog', { name: '新建任务' })
    await waitFor(() => expect(screen.getByRole('combobox', { name: '项目上下文' })).toHaveTextContent('公众号项目'))
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    expect(await screen.findByText('created task detail')).toBeInTheDocument()
  })

  it('marks attachment changes dirty so closing asks for confirmation', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([fixtures.project as Project])
    uploadToOSSMock.mockImplementation(async ({ file }: { file: File }) => ({
      uploadId: `upload-${file.name}`,
      key: `uploads/pending/user/${file.name}`,
      publicUrl: '',
      contentType: file.type,
      size: file.size,
    }))
    renderTasksPage('/tasks?create=true&type=article&project_id=project-1&intent=new')

    await screen.findByRole('dialog', { name: '新建任务' })
    fireEvent.change(screen.getByLabelText('选择附件文件'), {
      target: { files: [new File(['brief'], 'brief.pdf', { type: 'application/pdf' })] },
    })
    await screen.findByRole('button', { name: '预览 brief.pdf' })
    fireEvent.click(screen.getByRole('button', { name: '取消' }))

    expect(await screen.findByRole('alertdialog', { name: '放弃编辑？' })).toBeInTheDocument()
  })

})

describe('TasksPage URL-driven recovery filters', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.tasks.list).mockResolvedValue({ items: [fixtures.failedTask as Task], total: 1 })
    vi.mocked(api.projects.list).mockResolvedValue([fixtures.project as Project])
  })

  it('syncs recovery queue links into the task status filter', async () => {
    renderTasksPage()

    await waitFor(() => {
      expect(api.tasks.list).toHaveBeenCalledWith(expect.objectContaining({ status: undefined }))
    })

    fireEvent.click(await screen.findByRole('link', { name: /失败任务/ }))

    await waitFor(() => {
      expect(api.tasks.list).toHaveBeenCalledWith(expect.objectContaining({ status: 'failed' }))
    })
  })

  it('uses publish approval recovery labels and task card action signals', async () => {
    vi.mocked(api.tasks.list).mockResolvedValue({
      items: [fixtures.failedTask as Task, fixtures.approvalTask as Task],
      total: 2,
    })

    renderTasksPage()

    expect(await screen.findByRole('link', { name: /待发布确认/ })).toBeInTheDocument()
    expect(await screen.findByText('处理发布审批')).toBeInTheDocument()
    expect((await screen.findAllByText('审核后放行到公众号草稿箱')).length).toBeGreaterThan(0)
  })

  it('shows the task total including image operation charges', async () => {
    vi.mocked(api.tasks.list).mockResolvedValue({
      items: [{ ...fixtures.failedTask, billing_total_credits: 6800 } as Task],
      total: 1,
    })

    renderTasksPage()

    expect(await screen.findByText('累计扣费：6,800 积分')).toBeInTheDocument()
    expect(screen.queryByText('累计扣费：6,000 积分')).not.toBeInTheDocument()
  })

  it('uses the active catalog price for article tasks', async () => {
    vi.mocked(api.billing.wallet).mockResolvedValueOnce({ paid: 7000, promotional: 0, debt: 0, balance: 7000 })
    renderTasksPage('/tasks?create=true&type=article&project_id=project-1&intent=new')

    expect(await screen.findByRole('dialog', { name: '新建任务' })).toBeInTheDocument()
    expect(await screen.findByText(/固定任务价：6,000 × 1 =/)).toBeInTheDocument()
    expect(screen.getByText(/余额：7,000 →/)).toBeInTheDocument()
    expect(screen.getByText('1,000')).toBeInTheDocument()
    expect(screen.queryByText(/运行预留/)).not.toBeInTheDocument()
    expect(screen.queryByText(/充值后再创建/)).not.toBeInTheDocument()
  })

  it('blocks creation instead of treating an unavailable catalog price as zero', async () => {
    vi.mocked(api.billing.catalog)
      .mockRejectedValueOnce(new Error('catalog unavailable'))
      .mockRejectedValueOnce(new Error('catalog unavailable'))
    vi.mocked(api.billing.wallet).mockResolvedValueOnce({ paid: 7000, promotional: 0, debt: 0, balance: 7000 })
    renderTasksPage('/tasks?create=true&type=article&project_id=project-1&intent=new')

    fireEvent.click(await screen.findByRole('button', { name: /^性价比，/ }))
    expect(await screen.findByText('固定任务价暂不可用')).toBeInTheDocument()
    expect(screen.getByText('固定价格目录暂不可用，请稍后重试。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '创建' })).toBeDisabled()
    expect(screen.queryByText(/固定任务价：0/)).not.toBeInTheDocument()
  })

  it('shows ecommerce creation as a base task fee instead of a module package charge', async () => {
    vi.mocked(api.projects.list).mockResolvedValueOnce([{
      ...fixtures.project,
      id: 'project-ecommerce',
      platform: 'ecommerce',
      name: '电商项目',
    } as Project])

    renderTasksPage('/tasks?create=true&type=ecommerce&project_id=project-ecommerce&intent=new')

    expect(await screen.findByRole('dialog', { name: '新建任务' })).toBeInTheDocument()
    expect(await screen.findByText(/固定任务价：3,000 × 1 =/)).toBeInTheDocument()
    expect(screen.queryByText(/模块套餐预估/)).not.toBeInTheDocument()
    expect(screen.getByText(/成功交付的图片、视频等增值操作/)).toBeInTheDocument()
  })
})

describe('TasksPage bulk clone execution profile', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.tasks.list).mockResolvedValue({ items: [fixtures.failedTask as Task], total: 1 })
    vi.mocked(api.projects.list).mockResolvedValue([fixtures.project as Project])
    vi.mocked(api.billing.catalog).mockResolvedValue({
      catalog_id: 'retail-test-v1',
      currency: 'credits',
      skus: [
        { id: 'task.article.effective', operation: 'task.article', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 4800, delivery: 'article_artifacts_verified' },
        { id: 'task.article.balanced', operation: 'task.article', execution_profile: 'balanced', charge_policy: 'task_admission', price_credits: 6000, delivery: 'article_artifacts_verified' },
        { id: 'task.article.quality', operation: 'task.article', execution_profile: 'quality', charge_policy: 'task_admission', price_credits: 18000, delivery: 'article_artifacts_verified' },
      ],
    })
  })

  afterEach(() => {
    vi.mocked(api.billing.catalog).mockResolvedValue({
      catalog_id: 'retail-test-v1',
      currency: 'credits',
      skus: [
        { id: 'task.article.effective', operation: 'task.article', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 6000, delivery: 'article_artifacts_verified' },
        { id: 'task.seednote.effective', operation: 'task.seednote', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 5000, delivery: 'seednote_artifacts_verified' },
        { id: 'task.ecommerce.effective', operation: 'task.ecommerce', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 3000, delivery: 'ecommerce_artifacts_verified' },
        { id: 'task.montage.effective', operation: 'task.montage', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 2000, delivery: 'montage_artifacts_verified' },
      ],
    })
  })

  async function openBulkCloneDialog() {
    renderTasksPage()
    fireEvent.click(await screen.findByRole('button', { name: '选择任务' }))
    fireEvent.click(screen.getByRole('button', { name: '克隆 (1)' }))
    return screen.findByRole('alertdialog', { name: '批量克隆任务？' })
  }

  it('defaults to the cheapest available server profile and submits it', async () => {
    vi.mocked(api.tasks.bulkClone).mockResolvedValueOnce({ total: 1, succeeded: 1, skipped: 0, results: [] })
    const dialog = await openBulkCloneDialog()

    expect(within(dialog).getByText('执行配置')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: /^性价比，全部用户/ })).toHaveAttribute('aria-pressed', 'true')
    expect(within(dialog).queryByText('deepseek-v4-flash')).not.toBeInTheDocument()
    expect(within(dialog).getByText('预计总计 4,800 积分')).toBeInTheDocument()

    fireEvent.click(within(dialog).getByRole('button', { name: '确认克隆' }))

    await waitFor(() => expect(api.tasks.bulkClone).toHaveBeenCalledWith(['failed-task'], 'effective'))
  })

  it('disables confirmation when no available profile has a matching SKU', async () => {
    vi.mocked(api.billing.catalog).mockResolvedValueOnce({
      catalog_id: 'retail-test-v1',
      currency: 'credits',
      skus: [],
    })
    const dialog = await openBulkCloneDialog()

    expect(within(dialog).getByRole('button', { name: '确认克隆' })).toBeDisabled()
    expect(within(dialog).getByText('暂时无法获取所选配置的任务价格')).toBeInTheDocument()
  })

  it('keeps an explicit profile selection visible when its SKU is missing', async () => {
    vi.mocked(api.billing.catalog).mockResolvedValueOnce({
      catalog_id: 'retail-test-v1',
      currency: 'credits',
      skus: [
        { id: 'task.article.effective', operation: 'task.article', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 4800, delivery: 'article_artifacts_verified' },
      ],
    })
    const dialog = await openBulkCloneDialog()
    const balanced = within(dialog).getByRole('button', { name: /^平衡型，Pro 版及以上/ })

    fireEvent.click(balanced)

    await waitFor(() => expect(balanced).toHaveAttribute('aria-pressed', 'true'))
    expect(within(dialog).getByRole('button', { name: '确认克隆' })).toBeDisabled()
    expect(within(dialog).getByText('暂时无法获取所选配置的任务价格')).toBeInTheDocument()
  })
})


describe('TasksPage Seednote reference materials', () => {
  const seednoteProject = {
    ...fixtures.project,
    id: 'project-seednote',
    platform: 'seednote',
    name: '种草项目',
    image_ratio: '3:4',
  } as Project

  const ecommerceProject = {
    ...fixtures.project,
    id: 'project-ecommerce',
    platform: 'ecommerce',
    name: '电商项目',
  } as Project

  beforeEach(() => {
    vi.clearAllMocks()
    uploadToOSSMock.mockImplementation(async ({ file }: { file: File }) => ({
      uploadId: `upload-${file.name}`,
      key: `uploads/pending/user/${file.name}`,
      publicUrl: `https://cdn.example/${file.name}?signed=secret`,
      contentType: file.type,
      size: file.size,
    }))
    vi.mocked(api.tasks.list).mockResolvedValue({ items: [], total: 0 })
    vi.mocked(api.billing.wallet).mockResolvedValue({ paid: 10000, promotional: 0, debt: 0, balance: 10000 })
    vi.mocked(api.tasks.create).mockResolvedValue({
      ...fixtures.failedTask,
      id: 'created-seednote-task',
      type: 'seednote',
      title: '新建种草笔记',
      prompt: '',
      status: 'pending',
      project_id: seednoteProject.id,
    } as Task)
  })

  it('configures Seednote image references and submits their snapshot', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([seednoteProject])
    const file = new File(['product'], 'product.png', { type: 'image/png' })

    renderTasksPage(`/tasks?create=true&type=seednote&project_id=${seednoteProject.id}&intent=new`)

    expect(await screen.findByRole('dialog', { name: '新建任务' })).toBeInTheDocument()
    expect(document.querySelector('[data-slot="agent-prompt-input"]')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('选择附件文件'), { target: { files: [file] } })
    await screen.findByText('product.png')
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() => {
      expect(api.tasks.create).toHaveBeenCalledWith(expect.objectContaining({
        type: 'seednote',
        input_attachments: [{
          type: 'image',
          upload_id: 'upload-product.png',
          key: 'uploads/pending/user/product.png',
          file_name: 'product.png',
          content_type: 'image/png',
          size: file.size,
        }],
      }))
    })
  })

  it('creates a task with an ordered image attachment and no legacy reference field', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([seednoteProject])
    let resolveReferenceUpload!: (value: unknown) => void
    uploadToOSSMock.mockImplementationOnce(() => new Promise((resolve) => {
      resolveReferenceUpload = resolve
    }))

    renderTasksPage(`/tasks?create=true&type=seednote&project_id=${seednoteProject.id}&intent=new`)

    await screen.findByRole('dialog', { name: '新建任务' })
    fireEvent.change(screen.getByLabelText('选择附件文件'), {
      target: { files: [new File(['reference'], 'task-reference.png', { type: 'image/png' })] },
    })
    expect(screen.getByRole('button', { name: '创建' })).toBeDisabled()
    fireEvent.submit(document.getElementById('task-create-form')!)
    expect(api.tasks.create).not.toHaveBeenCalled()

    await act(async () => {
      resolveReferenceUpload({
        uploadSessionId: '33333333-3333-4333-8333-333333333333',
        uploadId: 'upload-task-reference',
        key: 'uploads/pending/task-reference.png',
        publicUrl: 'https://staging.example/task-reference.png',
        previewUrl: 'https://staging.example/task-reference.png',
        contentType: 'image/png',
        size: 9,
      })
      await Promise.resolve()
    })
    await waitFor(() => expect(screen.getByRole('button', { name: '创建' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() => expect(api.tasks.create).toHaveBeenCalled())
    const payload = vi.mocked(api.tasks.create).mock.calls[0][0]
    expect(payload.input_attachments).toEqual([expect.objectContaining({
      type: 'image',
      upload_id: 'upload-task-reference',
      key: 'uploads/pending/task-reference.png',
      file_name: 'task-reference.png',
    })])
    expect(payload).not.toHaveProperty('reference_image')
    expect(payload).not.toHaveProperty('reference_image_url')
  })

  it('keeps the ordered image attachment open when task creation fails', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([seednoteProject])
    uploadToOSSMock.mockResolvedValueOnce({
      uploadSessionId: '77777777-7777-4777-8777-777777777777',
      uploadId: 'upload-expired-task-reference',
      key: 'uploads/pending/expired-task-reference.png',
      publicUrl: 'https://staging.example/expired-task-reference.png',
      previewUrl: 'https://staging.example/expired-task-reference.png',
      contentType: 'image/png',
      size: 9,
    })
    vi.mocked(api.tasks.create).mockRejectedValueOnce({
      response: { data: { msg: '任务参考图会话已过期，请重新上传' } },
    })
    renderTasksPage(`/tasks?create=true&type=seednote&project_id=${seednoteProject.id}&intent=new`)

    await screen.findByRole('dialog', { name: '新建任务' })
    fireEvent.change(screen.getByLabelText('选择附件文件'), {
      target: { files: [new File(['reference'], 'expired-task.png', { type: 'image/png' })] },
    })
    const preview = await screen.findByRole('button', { name: '预览 expired-task.png' })
    await waitFor(() => expect(screen.getByRole('button', { name: '创建' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() => expect(errorMock).toHaveBeenCalledWith('任务参考图会话已过期，请重新上传'))
    expect(screen.getByRole('dialog', { name: '新建任务' })).toBeInTheDocument()
    expect(preview).toBeInTheDocument()
    const payload = vi.mocked(api.tasks.create).mock.calls[0][0]
    expect(payload.input_attachments).toEqual([expect.objectContaining({
      type: 'image',
      upload_id: 'upload-expired-task-reference',
      key: 'uploads/pending/expired-task-reference.png',
      file_name: 'expired-task.png',
    })])
    expect(payload).not.toHaveProperty('reference_image')
    expect(payload).not.toHaveProperty('reference_image_url')
  })

  it('guards native form submit until Seednote reference uploads finish', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([seednoteProject])

    let resolveUpload!: (value: unknown) => void
    uploadToOSSMock.mockImplementationOnce(() => new Promise((resolve) => { resolveUpload = resolve }))
    renderTasksPage(`/tasks?create=true&type=seednote&project_id=${seednoteProject.id}&intent=new`)

    expect(await screen.findByRole('dialog', { name: '新建任务' })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('选择附件文件'), {
      target: { files: [new File(['pending'], 'pending.png', { type: 'image/png' })] },
    })

    expect(screen.getByRole('button', { name: '创建' })).toBeDisabled()
    const form = document.getElementById('task-create-form')
    expect(form).toBeInstanceOf(HTMLFormElement)
    fireEvent.submit(form!)
    await act(async () => { await Promise.resolve() })
    expect(api.tasks.create).not.toHaveBeenCalled()

    await act(async () => {
      resolveUpload({ uploadId: 'pending', key: 'uploads/pending/pending.png', publicUrl: '', contentType: 'image/png', size: 7 })
      await Promise.resolve()
    })

    await waitFor(() => expect(screen.getByRole('button', { name: '创建' })).toBeEnabled())
    fireEvent.submit(form!)
    await waitFor(() => expect(api.tasks.create).toHaveBeenCalledWith(expect.objectContaining({
      input_attachments: [expect.objectContaining({
        upload_id: 'pending',
        key: 'uploads/pending/pending.png',
      })],
    })))
  })

  it('keeps the ecommerce product-photo uploader isolated from Seednote references', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([ecommerceProject])

    renderTasksPage(`/tasks?create=true&type=ecommerce&project_id=${ecommerceProject.id}&intent=new`)

    expect(await screen.findByRole('dialog', { name: '新建任务' })).toBeInTheDocument()
    expect(await screen.findByText('添加产品图')).toBeInTheDocument()
    expect(document.querySelector('[data-slot="agent-prompt-input"]')).toBeInTheDocument()
  })
})

describe('TasksPage Montage creation', () => {
  const montageProject = {
    ...fixtures.project,
    id: 'project-montage',
    platform: 'montage',
    name: 'Montage 项目',
    montage_defaults: {
      default_pipeline: 'social-short',
      preferences: {
        aspect_ratio: '16:9',
        duration_seconds: 45,
        style: 'clean product film',
        music_prompt: 'minimal electronic',
        subtitle_mode: 'burned-in',
        voiceover_mode: 'narrated',
      },
      asset_guidance: '优先使用实拍素材',
      delivery_targets: ['final_video', 'subtitles'],
    },
  } as Project

  beforeEach(() => {
    vi.clearAllMocks()
    referenceMaterialInputHarness.props = undefined
    vi.mocked(api.tasks.list).mockResolvedValue({ items: [], total: 0 })
    vi.mocked(api.projects.list).mockResolvedValue([montageProject])
    vi.mocked(api.billing.wallet).mockResolvedValue({ paid: 10000, promotional: 0, debt: 0, balance: 10000 })
    vi.mocked(api.tasks.create).mockResolvedValue({
      ...fixtures.failedTask,
      id: 'created-montage-task',
      type: 'montage',
      title: '新品发布短片',
      project_id: montageProject.id,
      status: 'pending',
    } as Task)
  })

  it('inherits project defaults and submits the complete Montage input', async () => {
    renderTasksPage(`/tasks?create=true&type=montage&project_id=${montageProject.id}&intent=new`)

    expect(await screen.findByRole('dialog', { name: '新建任务' })).toBeInTheDocument()
    expect(await screen.findByDisplayValue('social-short')).toBeInTheDocument()
    expect(screen.getByLabelText('时长（秒）')).toHaveValue(45)
    expect(screen.getByLabelText('音乐提示')).toHaveValue('minimal electronic')
    expect(screen.getByLabelText('字幕模式')).toHaveValue('burned-in')
    expect(screen.getByLabelText('配音模式')).toHaveValue('narrated')
    expect(screen.getByText('final_video')).toBeInTheDocument()
    await waitFor(() => expect(referenceMaterialInputHarness.props?.uploadPurpose).toBe('montage_asset'))

    fireEvent.change(screen.getByPlaceholderText('描述创作目标、内容要求和素材使用方式...'), {
      target: { value: '新品发布短片' },
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

    await waitFor(() => expect(api.tasks.create).toHaveBeenCalledWith(expect.objectContaining({
      type: 'montage',
      project_id: montageProject.id,
      montage_input: {
        brief: '新品发布短片',
        pipeline_key: 'social-short',
        source_assets: [{
          type: 'video_url',
          url: '/source.mp4',
          file_name: 'source.mp4',
          mime_type: 'video/mp4',
          file_size: 42,
        }],
        preferences: {
          aspect_ratio: '16:9',
          duration_seconds: 45,
          style: 'clean product film',
          music_prompt: 'minimal electronic',
          subtitle_mode: 'burned-in',
          voiceover_mode: 'narrated',
        },
        delivery_targets: ['final_video', 'subtitles'],
        advanced: undefined,
      },
    })))
  })

  it('blocks Montage creation while source assets are uploading', async () => {
    renderTasksPage(`/tasks?create=true&type=montage&project_id=${montageProject.id}&intent=new`)

    expect(await screen.findByRole('dialog', { name: '新建任务' })).toBeInTheDocument()
    await waitFor(() => expect(referenceMaterialInputHarness.props?.uploadPurpose).toBe('montage_asset'))

    act(() => {
      referenceMaterialInputHarness.props?.onUploadingChange?.(true)
    })

    expect(screen.getByRole('button', { name: '创建' })).toBeDisabled()
  })
})
