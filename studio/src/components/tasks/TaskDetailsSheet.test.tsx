import { createRef, useEffect, useState } from 'react'
import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import type { Project, Task } from '@/types'
import { TaskConfigurationDetails } from './TaskConfigurationDetails'
import { TaskDetailsSheet, type TaskDetailsSheetProps } from './TaskDetailsSheet'

const project: Project = {
  id: 'project-1',
  user_id: 'user-1',
  platform: 'article',
  name: '当前项目名称',
  avatar_url: '',
  profile_url: '',
  keywords: '',
  visual_style: '当前项目视觉风格',
  writer: '当前项目写作风格',
  theme: '当前项目排版',
  author: '当前项目作者',
  template_id: '',
  image_ratio: '16:9',
  max_concurrent_tasks: 1,
  config: {},
  status: 'active',
  created_at: '2026-07-01T00:00:00.000Z',
  updated_at: '2026-07-01T00:00:00.000Z',
}

const articleTask: Task = {
  id: 'task-1',
  type: 'article',
  title: '夏日选题',
  prompt: '写一篇夏日生活文章',
  status: 'completed',
  plan_id: null,
  project_id: project.id,
  project_snapshot: {
    project_name: '创建时项目名称',
    platform: 'article',
    visual_style: '柔光生活摄影',
    image_ratio: '3:2',
    author: '安班编辑部',
    writer: '真诚叙事',
    theme: '简约留白',
  },
  image_model_key: 'gpt-image-2',
  input_attachments: [{
    type: 'image',
    file_name: 'summer-reference.png',
    url: 'https://cdn.example.com/summer-reference.png',
    instruction: '保留柔和自然光',
  }],
  result: null,
  published: false,
  published_at: null,
  billing_price_credits: 6000,
  created_at: '2026-07-15T01:02:03.000Z',
  started_at: '2026-07-15T01:03:04.000Z',
  completed_at: '2026-07-15T01:08:09.000Z',
}

afterEach(() => {
  vi.restoreAllMocks()
})

function createSheetProps(overrides: Partial<TaskDetailsSheetProps> = {}): TaskDetailsSheetProps {
  return {
    open: true,
    onOpenChange: vi.fn(),
    selectedTab: 'overview',
    onTabChange: vi.fn(),
    task: articleTask,
    project,
    files: [],
    logs: ['## 阶段日志', '- 已完成选题'],
    sseError: null,
    autoScrollLogs: true,
    onToggleAutoScroll: vi.fn(),
    onCopyLogs: vi.fn(),
    onReconnectLogs: vi.fn(),
    logContainerRef: createRef<HTMLDivElement>(),
    ...overrides,
  }
}

function ControlledTaskDetailsSheet(props: TaskDetailsSheetProps) {
  const [selectedTab, setSelectedTab] = useState(props.selectedTab)

  useEffect(() => {
    setSelectedTab(props.selectedTab)
  }, [props.selectedTab, props.task.id])

  return (
    <TaskDetailsSheet
      {...props}
      selectedTab={selectedTab}
      onTabChange={(tab) => {
        props.onTabChange(tab)
        setSelectedTab(tab)
      }}
    />
  )
}

describe('TaskDetailsSheet', () => {
  it('overrides the base side width so the sheet fills mobile viewports', () => {
    render(<TaskDetailsSheet {...createSheetProps()} />)

    const sheet = screen.getByRole('dialog', { name: '任务详情' })
    expect(sheet).toHaveClass(
      'data-[side=right]:w-full',
      'data-[side=right]:sm:max-w-xl',
    )
    expect(sheet).not.toHaveClass(
      'data-[side=right]:w-3/4',
      'data-[side=right]:sm:max-w-sm',
    )
  })

  it('shows compact task context in the header', () => {
    render(<TaskDetailsSheet {...createSheetProps({ selectedTab: 'logs' })} />)

    expect(screen.getByText('夏日选题')).toBeInTheDocument()
    expect(screen.getByText('创建时项目名称')).toBeInTheDocument()
    expect(screen.getByText('已完成')).toBeInTheDocument()
  })

  it('opens on Overview with semantic task values and cumulative billing', () => {
    const props = createSheetProps()

    render(<ControlledTaskDetailsSheet {...props} />)

    expect(screen.getByRole('heading', { name: '任务详情' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '概览' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByText('手动创建')).toBeInTheDocument()
    expect(within(screen.getByRole('tabpanel')).getByText('创建时项目名称')).toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: '积分明细' })).getAllByText('6,000 积分')).toHaveLength(2)
  })

  it('groups Overview into timing, project, and billing surfaces', () => {
    render(<ControlledTaskDetailsSheet {...createSheetProps()} />)

    expect(screen.getByRole('region', { name: '任务时间与来源' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '项目' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '积分明细' })).toBeInTheDocument()
  })

  it('places configuration, materials, and logs in named grouped surfaces', () => {
    render(<ControlledTaskDetailsSheet {...createSheetProps()} />)

    fireEvent.click(screen.getByRole('tab', { name: '配置' }))
    expect(screen.getByRole('region', { name: '创作配置详情' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: '素材' }))
    expect(screen.getByRole('region', { name: '参考素材详情' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: '日志' }))
    expect(screen.getByRole('region', { name: '执行动态' })).toBeInTheDocument()
  })

  it('renders only the transient asset view URL for a task reference', () => {
    render(<ControlledTaskDetailsSheet {...createSheetProps({
      task: {
        ...articleTask,
        project_snapshot: {
          ...articleTask.project_snapshot,
          reference_image_asset_id: '44444444-4444-4444-8444-444444444444',
        },
        reference_image: {
          asset_id: '44444444-4444-4444-8444-444444444444',
          file_name: 'reference.png',
          content_type: 'image/png',
          size: 9,
          download_url: 'https://signed.example/reference.png',
          download_expires_at: '2026-07-20T10:00:00Z',
        },
      },
    })} />)

    fireEvent.click(screen.getByRole('tab', { name: '素材' }))

    expect(screen.getByRole('img', { name: '参考图' })).toHaveAttribute(
      'src',
      'https://signed.example/reference.png',
    )
    expect(document.body.textContent).not.toContain('44444444-4444-4444-8444-444444444444')
  })

  it('keeps the empty log surface compact', () => {
    const props = createSheetProps({ logs: [] })
    render(<ControlledTaskDetailsSheet {...props} />)
    fireEvent.click(screen.getByRole('tab', { name: '日志' }))

    const logPanel = screen.getByRole('tabpanel', { name: '日志' })
    const logRegion = screen.getByRole('region', { name: '执行动态' })
    const logDetails = props.logContainerRef.current?.parentElement
    const emptyState = screen.getByText('等待输出中...').closest('[data-slot="empty"]')

    expect(logPanel).toHaveClass('flex', 'min-h-28', 'flex-none', 'flex-col')
    expect(logPanel).not.toHaveClass('min-h-0', 'flex-1', 'overflow-hidden')
    expect(logRegion).toHaveClass('rounded-lg', 'border', 'min-h-28', 'flex-none')
    expect(logRegion).not.toHaveClass('min-h-0', 'flex-1', 'overflow-hidden')
    expect(logDetails).toHaveClass('flex', 'min-h-28', 'flex-none', 'flex-col')
    expect(logDetails).not.toHaveClass('min-h-0', 'flex-1', 'overflow-hidden')
    expect(props.logContainerRef.current).toHaveClass(
      'min-h-28',
      'flex-none',
      'rounded-md',
      'bg-muted/30',
      'p-3',
    )
    expect(props.logContainerRef.current).not.toHaveClass('flex-1', 'overflow-y-auto')
    expect(emptyState).toHaveClass('min-h-28', 'flex-none', 'border-0', 'p-3')
    expect(emptyState).not.toHaveClass('flex-1')
    expect(screen.getByRole('button', { name: '复制日志' })).toBeDisabled()
  })

  it('keeps the log body as the constrained scrolling owner', () => {
    const props = createSheetProps()
    render(<ControlledTaskDetailsSheet {...props} />)
    fireEvent.click(screen.getByRole('tab', { name: '日志' }))

    const logPanel = screen.getByRole('tabpanel', { name: '日志' })
    const logRegion = screen.getByRole('region', { name: '执行动态' })
    const logDetails = props.logContainerRef.current?.parentElement

    expect(logPanel).toHaveClass('flex', 'min-h-0', 'flex-col', 'overflow-hidden')
    expect(logRegion).toHaveClass('flex', 'min-h-0', 'flex-1', 'flex-col')
    expect(logDetails).toHaveClass('flex', 'min-h-0', 'flex-1', 'flex-col')
    expect(props.logContainerRef.current).toHaveClass('flex-1', 'overflow-y-auto')
    expect(logRegion.querySelector('svg')).toHaveClass('size-4')
  })

  it('shows the immutable article snapshot on Configuration', () => {
    render(<ControlledTaskDetailsSheet {...createSheetProps()} />)

    fireEvent.click(screen.getByRole('tab', { name: '配置' }))

    expect(within(screen.getByRole('tabpanel')).getByText('创建时项目名称')).toBeInTheDocument()
    expect(screen.getByText('公众号文章')).toBeInTheDocument()
    expect(screen.getByText('柔光生活摄影')).toBeInTheDocument()
    expect(screen.getByText('3:2')).toBeInTheDocument()
    expect(screen.getByText('gpt-image-2')).toBeInTheDocument()
    expect(screen.getByText('安班编辑部')).toBeInTheDocument()
    expect(screen.getByText('真诚叙事')).toBeInTheDocument()
    expect(screen.getByText('简约留白')).toBeInTheDocument()
  })

  it('does not fill a present partial snapshot from the mutable current project', () => {
    const changedProject: Project = {
      ...project,
      name: '后来修改的项目名',
      visual_style: '后来修改的视觉风格',
      image_ratio: '1:1',
      author: '后来修改的作者',
      writer: '后来修改的写作风格',
      theme: '后来修改的排版',
      ecommerce_defaults: { image_model_key: 'later-image-model' },
    }
    const partialSnapshotTask: Task = {
      ...articleTask,
      image_model_key: undefined,
      project_snapshot: { platform: 'article' },
    }
    render(
      <ControlledTaskDetailsSheet
        {...createSheetProps({ task: partialSnapshotTask, project: changedProject })}
      />,
    )

    expect(screen.queryByText('后来修改的项目名')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('tab', { name: '配置' }))

    for (const currentValue of [
      '后来修改的项目名',
      '后来修改的视觉风格',
      '1:1',
      'later-image-model',
      '后来修改的作者',
      '后来修改的写作风格',
      '后来修改的排版',
    ]) {
      expect(screen.queryByText(currentValue)).not.toBeInTheDocument()
    }
  })

  it('uses current project values only for a legacy task without a snapshot', () => {
    const legacyProject: Project = {
      ...project,
      name: '旧任务当前项目',
      visual_style: '旧任务视觉风格',
      image_ratio: '4:3',
      author: '旧任务作者',
      writer: '旧任务写作风格',
      theme: '旧任务排版',
      ecommerce_defaults: { image_model_key: 'legacy-image-model' },
    }
    const legacyTask: Task = {
      ...articleTask,
      image_model_key: undefined,
      project_snapshot: undefined,
    }
    render(
      <ControlledTaskDetailsSheet
        {...createSheetProps({ task: legacyTask, project: legacyProject })}
      />,
    )

    expect(within(screen.getByRole('tabpanel')).getByText('旧任务当前项目')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('tab', { name: '配置' }))
    expect(within(screen.getByRole('tabpanel')).getByText('旧任务当前项目')).toBeInTheDocument()
    expect(screen.getByText('旧任务视觉风格')).toBeInTheDocument()
    expect(screen.getByText('4:3')).toBeInTheDocument()
    expect(screen.getByText('legacy-image-model')).toBeInTheDocument()
    expect(screen.getByText('旧任务作者')).toBeInTheDocument()
    expect(screen.getByText('旧任务写作风格')).toBeInTheDocument()
    expect(screen.getByText('旧任务排版')).toBeInTheDocument()
  })

  it('treats an empty snapshot object as legacy override plus current project data', () => {
    const legacyProject: Project = {
      ...project,
      name: '空快照当前项目',
      visual_style: '当前项目视觉不应覆盖',
      image_ratio: '5:4',
      author: '当前项目作者不应覆盖',
      writer: '当前项目写作不应覆盖',
      theme: '当前项目排版不应覆盖',
      ecommerce_defaults: { image_model_key: 'empty-snapshot-image-model' },
    }
    const legacyTask: Task = {
      ...articleTask,
      image_model_key: undefined,
      project_snapshot: {},
      overrides: {
        visual_style: '旧任务覆盖视觉',
        author: '旧任务覆盖作者',
        writer: '旧任务覆盖写作',
        theme: '旧任务覆盖排版',
      },
    }
    render(
      <ControlledTaskDetailsSheet
        {...createSheetProps({ task: legacyTask, project: legacyProject })}
      />,
    )

    expect(within(screen.getByRole('tabpanel')).getByText('空快照当前项目')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('tab', { name: '配置' }))
    expect(within(screen.getByRole('tabpanel')).getByText('空快照当前项目')).toBeInTheDocument()
    expect(screen.getByText('旧任务覆盖视觉')).toBeInTheDocument()
    expect(screen.getByText('旧任务覆盖作者')).toBeInTheDocument()
    expect(screen.getByText('旧任务覆盖写作')).toBeInTheDocument()
    expect(screen.getByText('旧任务覆盖排版')).toBeInTheDocument()
    expect(screen.getByText('5:4')).toBeInTheDocument()
    expect(screen.getByText('empty-snapshot-image-model')).toBeInTheDocument()
    expect(screen.queryByText('当前项目视觉不应覆盖')).not.toBeInTheDocument()
  })

  it('ignores conflicting legacy style overrides when the snapshot platform is valid', () => {
    const snapshotTask: Task = {
      ...articleTask,
      project_snapshot: {
        project_name: '冻结项目',
        platform: 'article',
        visual_style: '冻结视觉',
        image_ratio: '3:2',
        author: '冻结作者',
        writer: '冻结写作',
        theme: '冻结排版',
      },
      overrides: {
        visual_style: '冲突旧视觉',
        author: '冲突旧作者',
        writer: '冲突旧写作',
        theme: '冲突旧排版',
      },
    }

    render(<TaskConfigurationDetails task={snapshotTask} project={project} />)

    expect(screen.getByText('冻结视觉')).toBeInTheDocument()
    expect(screen.getByText('冻结作者')).toBeInTheDocument()
    expect(screen.getByText('冻结写作')).toBeInTheDocument()
    expect(screen.getByText('冻结排版')).toBeInTheDocument()
    expect(screen.queryByText('冲突旧视觉')).not.toBeInTheDocument()
    expect(screen.queryByText('冲突旧作者')).not.toBeInTheDocument()
    expect(screen.queryByText('冲突旧写作')).not.toBeInTheDocument()
    expect(screen.queryByText('冲突旧排版')).not.toBeInTheDocument()
  })

  it('shows the compact task input fallback on Materials', () => {
    render(<ControlledTaskDetailsSheet {...createSheetProps()} />)

    fireEvent.click(screen.getByRole('tab', { name: '素材' }))

    expect(screen.getByText('未生成素材使用结论，仅展示任务输入。')).toBeInTheDocument()
    expect(screen.getByText('summer-reference.png')).toBeInTheDocument()
    expect(screen.getByText('保留柔和自然光')).toBeInTheDocument()
  })

  it('renders Markdown logs and wires follow, copy, and reconnect actions', () => {
    const props = createSheetProps({ sseError: '实时连接已中断' })
    render(<ControlledTaskDetailsSheet {...props} />)

    fireEvent.click(screen.getByRole('tab', { name: '日志' }))

    expect(screen.getByText('2 条')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '阶段日志' })).toBeInTheDocument()
    expect(props.logContainerRef.current).toBeInstanceOf(HTMLDivElement)

    fireEvent.click(screen.getByRole('button', { name: '跟随输出' }))
    fireEvent.click(screen.getByRole('button', { name: '复制日志' }))
    fireEvent.click(screen.getByRole('button', { name: '重新连接' }))

    expect(props.onToggleAutoScroll).toHaveBeenCalledTimes(1)
    expect(props.onCopyLogs).toHaveBeenCalledTimes(1)
    expect(props.onReconnectLogs).toHaveBeenCalledTimes(1)
    expect(screen.getByText('实时连接已中断')).toHaveClass('min-w-0', 'break-words')
    expect(screen.getByRole('button', { name: '重新连接' })).toHaveClass('shrink-0')
  })

  it('scrolls existing logs to the bottom when Logs mounts from an inactive tab', () => {
    vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockReturnValue(640)
    const props = createSheetProps()
    render(<ControlledTaskDetailsSheet {...props} />)

    expect(props.logContainerRef.current).toBeNull()
    fireEvent.click(screen.getByRole('tab', { name: '日志' }))

    expect(props.logContainerRef.current?.scrollTop).toBe(640)
  })

  it('scrolls existing logs to the bottom when the controlled sheet reopens', async () => {
    vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockReturnValue(720)
    const props = createSheetProps()
    const { rerender } = render(<ControlledTaskDetailsSheet {...props} />)
    fireEvent.click(screen.getByRole('tab', { name: '日志' }))
    const firstLogContainer = props.logContainerRef.current
    expect(firstLogContainer?.scrollTop).toBe(720)

    rerender(<ControlledTaskDetailsSheet {...props} open={false} />)
    await waitFor(() => expect(props.logContainerRef.current).toBeNull())

    rerender(<ControlledTaskDetailsSheet {...props} open />)
    await waitFor(() => {
      expect(props.logContainerRef.current).not.toBe(firstLogContainer)
      expect(props.logContainerRef.current?.scrollTop).toBe(720)
    })
  })

  it('disables copying while waiting for the first log entry', () => {
    render(<ControlledTaskDetailsSheet {...createSheetProps({ logs: [] })} />)

    fireEvent.click(screen.getByRole('tab', { name: '日志' }))

    expect(screen.getByText('等待输出中...')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '复制日志' })).toBeDisabled()
  })

  it('renders the supplied tab and delegates tab changes to its owner', () => {
    const onTabChange = vi.fn()
    render(
      <ControlledTaskDetailsSheet
        {...createSheetProps({ selectedTab: 'logs', onTabChange })}
      />,
    )

    expect(screen.getByRole('tab', { name: '日志' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('heading', { name: '阶段日志' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: '配置' }))

    expect(onTabChange).toHaveBeenCalledWith('configuration')
    expect(screen.getByRole('tab', { name: '配置' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByText('柔光生活摄影')).toBeInTheDocument()
  })

  it('uses warm semantic styling for active tabs', () => {
    render(<TaskDetailsSheet {...createSheetProps()} />)

    for (const name of ['概览', '配置', '素材', '日志']) {
      expect(screen.getByRole('tab', { name })).toHaveClass(
        'data-active:text-primary',
        'data-active:after:bg-primary',
      )
    }
  })
})

describe('TaskConfigurationDetails', () => {
  it('keeps ecommerce task overrides but does not leak current project defaults into a snapshot', () => {
    const ecommerceProject: Project = {
      ...project,
      platform: 'ecommerce',
      ecommerce_defaults: {
        target_platform: '后来修改的平台',
        default_selected_modules: { current_module: 9 },
        brand_brief: '后来修改的品牌简述',
        image_model_key: 'later-ecommerce-model',
      },
    }
    const ecommerceTask: Task = {
      ...articleTask,
      id: 'ecommerce-task',
      type: 'ecommerce',
      image_model_key: undefined,
      project_snapshot: {
        project_name: '电商创建快照',
        platform: 'ecommerce',
        ecommerce_defaults: {},
      },
      ecommerce: {
        target_platform: 'Amazon',
        selected_modules: { hero: 2 },
      },
    }

    render(<TaskConfigurationDetails task={ecommerceTask} project={ecommerceProject} />)

    expect(screen.getByText('Amazon')).toBeInTheDocument()
    expect(screen.getByText('hero x2')).toBeInTheDocument()
    expect(screen.queryByText('后来修改的平台')).not.toBeInTheDocument()
    expect(screen.queryByText('current_module x9')).not.toBeInTheDocument()
    expect(screen.queryByText('后来修改的品牌简述')).not.toBeInTheDocument()
    expect(screen.queryByText('later-ecommerce-model')).not.toBeInTheDocument()
  })
})
