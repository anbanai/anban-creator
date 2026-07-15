import { createRef } from 'react'
import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
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
  reference_image_url: '',
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
  result: { files: null, output: '' },
  published: false,
  published_at: null,
  created_at: '2026-07-15T01:02:03.000Z',
  started_at: '2026-07-15T01:03:04.000Z',
  completed_at: '2026-07-15T01:08:09.000Z',
}

function createSheetProps(overrides: Partial<TaskDetailsSheetProps> = {}): TaskDetailsSheetProps {
  return {
    open: true,
    onOpenChange: vi.fn(),
    task: articleTask,
    project,
    files: [],
    netConsumedCredits: 188,
    showCreditDetails: true,
    onOpenCreditDetails: vi.fn(),
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

describe('TaskDetailsSheet', () => {
  it('opens on Overview with semantic task values and credit details action', () => {
    const props = createSheetProps()

    render(<TaskDetailsSheet {...props} />)

    expect(screen.getByRole('heading', { name: '任务详情' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '概览' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByText('手动创建')).toBeInTheDocument()
    expect(screen.getByText('创建时项目名称')).toBeInTheDocument()
    expect(screen.getByText('188')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '查看明细' }))
    expect(props.onOpenCreditDetails).toHaveBeenCalledTimes(1)
  })

  it('shows the immutable article snapshot on Configuration', () => {
    render(<TaskDetailsSheet {...createSheetProps()} />)

    fireEvent.click(screen.getByRole('tab', { name: '配置' }))

    expect(screen.getByText('创建时项目名称')).toBeInTheDocument()
    expect(screen.getByText('公众号文章')).toBeInTheDocument()
    expect(screen.getByText('柔光生活摄影')).toBeInTheDocument()
    expect(screen.getByText('3:2')).toBeInTheDocument()
    expect(screen.getByText('gpt-image-2')).toBeInTheDocument()
    expect(screen.getByText('安班编辑部')).toBeInTheDocument()
    expect(screen.getByText('真诚叙事')).toBeInTheDocument()
    expect(screen.getByText('简约留白')).toBeInTheDocument()
  })

  it('shows the compact task input fallback on Materials', () => {
    render(<TaskDetailsSheet {...createSheetProps()} />)

    fireEvent.click(screen.getByRole('tab', { name: '素材' }))

    expect(screen.getByText('未生成素材使用结论，仅展示任务输入。')).toBeInTheDocument()
    expect(screen.getByText('summer-reference.png')).toBeInTheDocument()
    expect(screen.getByText('保留柔和自然光')).toBeInTheDocument()
  })

  it('renders Markdown logs and wires follow, copy, and reconnect actions', () => {
    const props = createSheetProps({ sseError: '实时连接已中断' })
    render(<TaskDetailsSheet {...props} />)

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
  })

  it('disables copying while waiting for the first log entry', () => {
    render(<TaskDetailsSheet {...createSheetProps({ logs: [] })} />)

    fireEvent.click(screen.getByRole('tab', { name: '日志' }))

    expect(screen.getByText('等待输出中...')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '复制日志' })).toBeDisabled()
  })

  it('resets to Overview when a different task is rendered', () => {
    const props = createSheetProps()
    const { rerender } = render(<TaskDetailsSheet {...props} />)
    fireEvent.click(screen.getByRole('tab', { name: '日志' }))
    expect(screen.getByRole('tab', { name: '日志' })).toHaveAttribute('aria-selected', 'true')

    rerender(
      <TaskDetailsSheet
        {...props}
        task={{ ...articleTask, id: 'task-2', title: '另一个任务' }}
      />,
    )

    expect(screen.getByRole('tab', { name: '概览' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByText('手动创建')).toBeInTheDocument()
  })
})

describe('TaskConfigurationDetails', () => {
  it('keeps the complete video input and resolved creation snapshot visible', () => {
    const videoTask: Task = {
      ...articleTask,
      id: 'video-task',
      type: 'videocreator',
      prompt: '后备视频 brief',
      project_snapshot: {
        project_name: '视频项目快照',
        platform: 'videocreator',
        visual_style: '纪实镜头',
        image_ratio: '9:16',
      },
      video_creator_input: {
        brief: '做一条办公室个人 IP 种草视频',
        references: [{
          type: 'text',
          text: '不要卡通化',
          reference_role: 'style',
          input_duration_seconds: 8,
        }],
        hard_constraints: { ratio: '9:16', duration: 15, watermark: false },
      },
      video_creator_config: {
        creative_type: 'personal_ip',
        purpose: 'planting',
        subject_profile: '30 岁效率博主，黑色衬衫',
        audience: '职场新人',
        single_message: '把会议记录变成行动清单',
        model_key: 'seedance-2.0-mini',
        resolution: '720p',
        ratio: '9:16',
        duration: 15,
        estimated_credits: 7440,
        references: [{
          type: 'video_url',
          url: 'https://cdn.example.com/ref.mp4',
          reference_role: 'rhythm',
          input_duration_seconds: 60,
        }],
      },
      video_estimated_credits: 7600,
      video_credits_charged: 7550,
    }

    render(<TaskConfigurationDetails task={videoTask} project={project} />)

    expect(screen.getByRole('heading', { name: '用户输入' })).toBeInTheDocument()
    expect(screen.getByText('做一条办公室个人 IP 种草视频')).toBeInTheDocument()
    expect(screen.getByText('风格参考 · 不要卡通化')).toBeInTheDocument()
    expect(screen.getByText('输入时长 8s')).toBeInTheDocument()
    expect(screen.getByText('不加水印')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Agent 解析结果' })).toBeInTheDocument()
    expect(screen.getByText('个人 IP')).toBeInTheDocument()
    expect(screen.getByText('种草')).toBeInTheDocument()
    expect(screen.getByText('豆包 Seedance 2.0 Mini（轻量）')).toBeInTheDocument()
    expect(screen.getByText('720p · 9:16 · 15s')).toBeInTheDocument()
    expect(screen.getByText('30 岁效率博主，黑色衬衫')).toBeInTheDocument()
    expect(screen.getByText('职场新人')).toBeInTheDocument()
    expect(screen.getByText('把会议记录变成行动清单')).toBeInTheDocument()
    expect(screen.getByText('7,600')).toBeInTheDocument()
    expect(screen.getByText('7,550')).toBeInTheDocument()
    expect(screen.getByText('节奏参考 · https://cdn.example.com/ref.mp4')).toBeInTheDocument()
    expect(screen.getByText('输入时长 60s')).toBeInTheDocument()
  })
})
