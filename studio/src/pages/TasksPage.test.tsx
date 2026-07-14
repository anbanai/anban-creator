import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi, beforeEach } from 'vitest'

import TasksPage from './TasksPage'
import { api } from '@/lib/api'
import { createTestQueryClient } from '@/test/test-utils'
import type { ReferenceMaterialInputProps } from '@/components/ReferenceMaterialInput'
import type { InputAttachment, Project, Task } from '@/types'

vi.mock('sonner', () => ({ toast: { error: vi.fn(), message: vi.fn(), success: vi.fn() } }))

const referenceMaterialInputHarness = vi.hoisted(() => ({
  props: undefined as ReferenceMaterialInputProps | undefined,
}))

vi.mock('@/components/ReferenceMaterialInput', () => ({
  ReferenceMaterialInput: (props: ReferenceMaterialInputProps) => {
    referenceMaterialInputHarness.props = props
    return <div />
  },
}))

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
    reference_image_url: '',
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
    result: { files: null, output: '' },
    published: false,
    published_at: null,
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
    result: { files: null, output: '' },
    published: false,
    published_at: null,
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
      },
      credits: {
        ...actual.api.credits,
        balance: vi.fn().mockResolvedValue({ balance: 1000 }),
        pricing: vi.fn().mockResolvedValue({
          task_costs: {},
          agent_runtime_reserve: { article: 4000, ecommerce: 3000 },
          model_costs: {},
          ecommerce_module_prices: {},
          recharge_tiers: [
            { key: 'basic', label: '基础包', price_cny: 10, credits: 10000, bonus_credits: 0, enabled: true },
            { key: 'standard', label: '标准包', price_cny: 50, credits: 52000, bonus_credits: 2000, enabled: true },
            { key: 'pro', label: '进阶包', price_cny: 100, credits: 110000, bonus_credits: 10000, enabled: true },
          ],
          income: { daily_sign_in: 100, register_bonus: 1000, invite_reward: 1000 },
        }),
      },
      videoCreator: {
        ...actual.api.videoCreator,
        estimate: vi.fn(),
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
          <Routes>
            <Route path="/tasks" element={<TasksPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  }
}

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

  it('uses backend-matching fallback pricing for article tasks when pricing omits task costs', async () => {
    vi.mocked(api.credits.balance).mockResolvedValueOnce({ balance: 5000 })
    renderTasksPage('/tasks?create=true&type=article&project_id=project-1&intent=new')

    expect(await screen.findByRole('dialog', { name: '新建任务' })).toBeInTheDocument()
    expect(await screen.findByText(/基础任务费：4000 × 1 =/)).toBeInTheDocument()
    expect(screen.getByText(/余额：5,000 →/)).toBeInTheDocument()
    expect(screen.getByText('1,000')).toBeInTheDocument()
    expect(screen.queryByText(/运行预留/)).not.toBeInTheDocument()
    expect(screen.queryByText('积分不足，补充积分后再创建。')).not.toBeInTheDocument()
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
    expect(await screen.findByText(/基础任务费：3000 × 1 =/)).toBeInTheDocument()
    expect(screen.queryByText(/模块套餐预估/)).not.toBeInTheDocument()
    expect(screen.getByText(/所选交付模块会影响后续图片生成和理解操作用量/)).toBeInTheDocument()
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
    referenceMaterialInputHarness.props = undefined
    vi.mocked(api.tasks.list).mockResolvedValue({ items: [], total: 0 })
    vi.mocked(api.credits.balance).mockResolvedValue({ balance: 10000 })
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
    const attachments: InputAttachment[] = [{
      type: 'image',
      url: '/product.png',
      file_name: 'product.png',
      content_type: 'image/png',
      upload_id: 'upload-1',
      key: 'uploads/product.png',
      instruction: '保持包装和 Logo 准确',
    }]

    renderTasksPage(`/tasks?create=true&type=seednote&project_id=${seednoteProject.id}&intent=new`)

    expect(await screen.findByRole('dialog', { name: '新建任务' })).toBeInTheDocument()
    await waitFor(() => expect(referenceMaterialInputHarness.props).toBeDefined())
    expect(screen.getByRole('region', { name: 'Seednote 参考素材' })).toBeInTheDocument()
    expect(referenceMaterialInputHarness.props).toEqual(expect.objectContaining({
      allowedTypes: ['image'],
      maxCount: 16,
      instructionEnabled: true,
      instructionMaxLength: 1000,
    }))

    act(() => {
      referenceMaterialInputHarness.props?.onChange(attachments)
    })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() => {
      expect(api.tasks.create).toHaveBeenCalledWith(expect.objectContaining({
        type: 'seednote',
        input_attachments: attachments,
      }))
    })
  })

  it('blocks Seednote creation while reference images are uploading', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([seednoteProject])

    renderTasksPage(`/tasks?create=true&type=seednote&project_id=${seednoteProject.id}&intent=new`)

    expect(await screen.findByRole('dialog', { name: '新建任务' })).toBeInTheDocument()
    await waitFor(() => expect(referenceMaterialInputHarness.props).toBeDefined())

    act(() => {
      referenceMaterialInputHarness.props?.onUploadingChange?.(true)
    })

    expect(screen.getByRole('button', { name: '创建' })).toBeDisabled()
  })

  it('keeps the ecommerce product-photo uploader isolated from Seednote references', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([ecommerceProject])

    renderTasksPage(`/tasks?create=true&type=ecommerce&project_id=${ecommerceProject.id}&intent=new`)

    expect(await screen.findByRole('dialog', { name: '新建任务' })).toBeInTheDocument()
    expect(await screen.findByText('添加产品图')).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Seednote 参考素材' })).not.toBeInTheDocument()
    expect(referenceMaterialInputHarness.props).toBeUndefined()
  })
})
