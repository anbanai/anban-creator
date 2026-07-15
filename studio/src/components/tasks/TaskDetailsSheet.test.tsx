import { createRef } from 'react'
import { fireEvent, screen, waitFor } from '@testing-library/react'
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
  result: null,
  published: false,
  published_at: null,
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
  it('overrides the base side width so the sheet fills mobile viewports', () => {
    render(<TaskDetailsSheet {...createSheetProps()} />)

    const sheet = screen.getByRole('dialog', { name: '任务详情' })
    expect(sheet).toHaveClass('data-[side=right]:w-full', 'sm:max-w-xl')
    expect(sheet).not.toHaveClass('data-[side=right]:w-3/4')
  })

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
      <TaskDetailsSheet
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
      <TaskDetailsSheet {...createSheetProps({ task: legacyTask, project: legacyProject })} />,
    )

    expect(screen.getByText('旧任务当前项目')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('tab', { name: '配置' }))
    expect(screen.getByText('旧任务当前项目')).toBeInTheDocument()
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
      <TaskDetailsSheet {...createSheetProps({ task: legacyTask, project: legacyProject })} />,
    )

    expect(screen.getByText('空快照当前项目')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('tab', { name: '配置' }))
    expect(screen.getByText('空快照当前项目')).toBeInTheDocument()
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
    expect(screen.getByText('实时连接已中断')).toHaveClass('min-w-0', 'break-words')
    expect(screen.getByRole('button', { name: '重新连接' })).toHaveClass('shrink-0')
  })

  it('scrolls existing logs to the bottom when Logs mounts from an inactive tab', () => {
    vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockReturnValue(640)
    const props = createSheetProps()
    render(<TaskDetailsSheet {...props} />)

    expect(props.logContainerRef.current).toBeNull()
    fireEvent.click(screen.getByRole('tab', { name: '日志' }))

    expect(props.logContainerRef.current?.scrollTop).toBe(640)
  })

  it('scrolls existing logs to the bottom when the controlled sheet reopens', async () => {
    vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockReturnValue(720)
    const props = createSheetProps()
    const { rerender } = render(<TaskDetailsSheet {...props} />)
    fireEvent.click(screen.getByRole('tab', { name: '日志' }))
    const firstLogContainer = props.logContainerRef.current
    expect(firstLogContainer?.scrollTop).toBe(720)

    rerender(<TaskDetailsSheet {...props} open={false} />)
    await waitFor(() => expect(props.logContainerRef.current).toBeNull())

    rerender(<TaskDetailsSheet {...props} open />)
    await waitFor(() => {
      expect(props.logContainerRef.current).not.toBe(firstLogContainer)
      expect(props.logContainerRef.current?.scrollTop).toBe(720)
    })
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
        scenario_key: 'office_story',
        production_mode: 'sequence',
        creative_type: 'personal_ip',
        purpose: 'planting',
        subject_profile: '30 岁效率博主，黑色衬衫',
        audience: '职场新人',
        single_message: '把会议记录变成行动清单',
        model_key: 'seedance-2.0-mini',
        resolution: '720p',
        ratio: '9:16',
        duration: 15,
        target_duration_source: 'ai_planned',
        target_duration_reason: '按口播节奏拆分为两个连续片段',
        segment_min_duration_seconds: 3,
        segment_max_duration_seconds: 8,
        watermark: false,
        preflight: true,
        retake_budget: 2,
        delivery_targets: ['final_video', 'quality_review'],
        estimated_credits: 7440,
        segments: [{
          index: 1,
          start_second: 0,
          end_second: 8,
          duration: 8,
          prompt: '办公室开场并展示行动清单',
          model_key: 'seedance-2.0-mini',
          resolution: '720p',
          ratio: '9:16',
          estimated_credits: 3600,
        }],
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
    expect(screen.getByText('office_story')).toBeInTheDocument()
    expect(screen.getByText('sequence')).toBeInTheDocument()
    expect(screen.getByText('ai_planned')).toBeInTheDocument()
    expect(screen.getByText('按口播节奏拆分为两个连续片段')).toBeInTheDocument()
    expect(screen.getByText('3s')).toBeInTheDocument()
    expect(screen.getByText('8s')).toBeInTheDocument()
    expect(screen.getByText('final_video、quality_review')).toBeInTheDocument()
    expect(screen.getByText('#1 · 0–8s · 时长 8s')).toBeInTheDocument()
    expect(screen.getByText('办公室开场并展示行动清单')).toBeInTheDocument()
    expect(screen.getByText(/豆包 Seedance 2\.0 Mini（轻量） · 720p · 9:16 · 3,600 积分/)).toBeInTheDocument()
    expect(screen.getByText('节奏参考 · https://cdn.example.com/ref.mp4')).toBeInTheDocument()
    expect(screen.getByText('输入时长 60s')).toBeInTheDocument()
  })

  it('shows video editor audit config when omitted fields are false or zero', () => {
    const editorTask: Task = {
      ...articleTask,
      id: 'video-editor-task',
      type: 'videoeditor',
      project_snapshot: { platform: 'videoeditor' },
      video_editor_config: {
        production_mode: 'guided',
        target_duration_seconds: 0,
        segment_min_duration_seconds: 0,
        segment_max_duration_seconds: 0,
        watermark: false,
        preflight: false,
        retake_budget: 0,
        delivery_targets: ['final_video'],
      },
    }

    render(<TaskConfigurationDetails task={editorTask} project={project} />)

    expect(screen.getByRole('heading', { name: 'Agent 解析结果' })).toBeInTheDocument()
    expect(screen.getByText('guided')).toBeInTheDocument()
    expect(screen.getByText('final_video')).toBeInTheDocument()
    expect(screen.getByText('规格').parentElement).toHaveTextContent('0s')
    expect(screen.getByText('最短分段').parentElement).toHaveTextContent('0s')
    expect(screen.getByText('最长分段').parentElement).toHaveTextContent('0s')
    expect(screen.getByText('返修预算').parentElement).toHaveTextContent('0')
    expect(screen.getByText('水印').parentElement).toHaveTextContent('关闭')
    expect(screen.getByText('预检').parentElement).toHaveTextContent('关闭')
  })

  it('renders authoritative pricing and complete resolved reference audit data', () => {
    const pricedVideoTask: Task = {
      ...articleTask,
      id: 'priced-video-task',
      type: 'videocreator',
      image_model_key: undefined,
      project_snapshot: { platform: 'videocreator' },
      video_creator_config: {
        pricing_breakdown: {
          cny: 0,
          credits_per_cny: 100,
          credit_multiplier: 0,
          tier_multiplier: 1.25,
          user_multiplier: 0.8,
          input_video: false,
          input_seconds: 0,
          output_seconds: 12,
          segment_count: 2,
          resolution: '1080p',
          ratio: '16:9',
          model_key: 'seedance-2.0-fast',
          segments: [
            { index: 1, seconds: 0, cny: 0, credits: 0 },
            { index: 2, seconds: 12, cny: 2.5, credits: 250 },
          ],
        },
        references: [
          {
            type: 'video_url',
            url: 'https://cdn.example.com/source.mp4',
            task_file_id: 'task-file-42',
            reference_role: 'full remake reference',
            must_keep: ['产品 Logo', '人物身份'],
            can_change: ['背景环境'],
            must_not_transfer: ['平台水印'],
            file_name: 'source.mp4',
            mime_type: 'video/mp4',
            file_size: 1048576,
            input_duration_seconds: 0,
          },
          {
            type: 'text',
            text: '只复刻节奏，不复刻人物',
            reference_role: 'rhythm',
          },
        ],
      },
    }

    render(<TaskConfigurationDetails task={pricedVideoTask} project={project} />)

    expect(screen.getByText('豆包 Seedance 2.0 Fast（快速）')).toBeInTheDocument()
    expect(screen.getByText('1080p · 16:9 · 12s')).toBeInTheDocument()
    expect(screen.getByText('计价金额').parentElement).toHaveTextContent('0 CNY')
    expect(screen.getByText('每元积分').parentElement).toHaveTextContent('100')
    expect(screen.getByText('积分倍率').parentElement).toHaveTextContent('0')
    expect(screen.getByText('档位倍率').parentElement).toHaveTextContent('1.25')
    expect(screen.getByText('用户倍率').parentElement).toHaveTextContent('0.8')
    expect(screen.getByText('输入视频').parentElement).toHaveTextContent('关闭')
    expect(screen.getByText('计价输入时长').parentElement).toHaveTextContent('0s')
    expect(screen.getByText('计价输出时长').parentElement).toHaveTextContent('12s')
    expect(screen.getByText('计价分段数').parentElement).toHaveTextContent('2')
    expect(screen.getByText('#1 · 0s · 0 CNY · 0 积分')).toBeInTheDocument()
    expect(screen.getByText('#2 · 12s · 2.5 CNY · 250 积分')).toBeInTheDocument()

    expect(screen.getByText('完整复刻参考')).toBeInTheDocument()
    expect(screen.getByText('source.mp4')).toBeInTheDocument()
    expect(screen.getByText('https://cdn.example.com/source.mp4')).toBeInTheDocument()
    expect(screen.getByText('task-file-42')).toBeInTheDocument()
    expect(screen.getByText('video/mp4')).toBeInTheDocument()
    expect(screen.getByText('1,048,576 bytes')).toBeInTheDocument()
    expect(screen.getByText('输入时长 0s')).toBeInTheDocument()
    expect(screen.getByText('产品 Logo、人物身份')).toBeInTheDocument()
    expect(screen.getByText('背景环境')).toBeInTheDocument()
    expect(screen.getByText('平台水印')).toBeInTheDocument()
    expect(screen.getByText('只复刻节奏，不复刻人物')).toBeInTheDocument()
  })

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
