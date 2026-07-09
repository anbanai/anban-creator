import { fireEvent, render, screen } from '@testing-library/react'
import { useWatch, useForm } from 'react-hook-form'
import { describe, expect, it } from 'vitest'
import { Form, FormControl, FormField, FormItem } from '@/components/ui/form'
import { Textarea } from '@/components/ui/textarea'
import type { Project, VideoInput } from '@/types'
import { VideoCreationPanel } from './VideoCreationPanel'

type PanelFormValues = {
  prompt: string
  video_creator_input: VideoInput
  video_editor_input: VideoInput
}

const videoProject = {
  id: 'project-1',
  name: '产品视频项目',
} as Project

function PanelHarness({ fieldRoot = 'video_creator_input' }: { fieldRoot?: 'video_creator_input' | 'video_editor_input' }) {
  const form = useForm<PanelFormValues>({
    defaultValues: {
      prompt: '',
      video_creator_input: {
        references: [{ type: 'text', text: '保持杯身银色' }],
        hard_constraints: {},
      },
      video_editor_input: {
        references: [{ type: 'video_url', url: 'https://cdn.example.com/source.mp4', reference_role: 'rhythm' }],
        hard_constraints: {},
      },
    },
  })
  const videoInput = useWatch({ control: form.control, name: fieldRoot })

  return (
    <Form {...form}>
      <VideoCreationPanel
        form={form}
        fieldRoot={fieldRoot}
        selectedProject={videoProject}
        title="AI 视频生成"
        promptField={(
          <FormField control={form.control} name="prompt" render={({ field }) => (
            <FormItem>
              <FormControl>
                <Textarea {...field} />
              </FormControl>
            </FormItem>
          )} />
        )}
      />
      <output data-testid="video-input">{JSON.stringify(videoInput)}</output>
    </Form>
  )
}

describe('VideoCreationPanel', () => {
  it('renders intake fields without old business configuration controls', () => {
    render(<PanelHarness />)

    expect(screen.getByText('本次视频要求')).toBeInTheDocument()
    expect(screen.getByText('参考素材')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '主体不变' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '完整复刻参考' })).toBeInTheDocument()
    expect(screen.getAllByText('保持杯身银色').length).toBeGreaterThan(0)
    for (const oldLabel of ['视频玩法', '制作模式', '工作流', '商业目标', '人物 / 主体', '目标受众', '核心信息']) {
      expect(screen.queryByText(oldLabel)).not.toBeInTheDocument()
    }
  })

  it('stores only hard constraints in video_creator_input', async () => {
    render(<PanelHarness />)

    fireEvent.click(screen.getByRole('button', { name: /高级硬约束/ }))
    fireEvent.change(screen.getByLabelText('时长（秒）'), { target: { value: '12' } })
    fireEvent.click(screen.getByLabelText('加水印'))

    expect(screen.getByTestId('video-input')).toHaveTextContent('"duration":12')
    expect(screen.getByTestId('video-input')).toHaveTextContent('"watermark":true')
    expect(screen.getByTestId('video-input')).not.toHaveTextContent('purpose')
    expect(screen.getByTestId('video-input')).not.toHaveTextContent('workflow')
  })

  it('adds subject identity as a text reference without blocking media-free creation', () => {
    render(<PanelHarness />)

    fireEvent.click(screen.getByRole('button', { name: '主体不变' }))

    expect(screen.getByTestId('video-input')).toHaveTextContent('"reference_role":"subject identity"')
    expect(screen.getByTestId('video-input')).toHaveTextContent('"type":"text"')
    expect(screen.getByText('主体不变最好上传主体图片或视频；没有素材也可以继续凭空创作，Agent 会按文字约束执行。')).toBeInTheDocument()
  })

  it('writes quick text references to the selected video input root', () => {
    render(<PanelHarness fieldRoot="video_editor_input" />)

    fireEvent.click(screen.getByRole('button', { name: '声音/BGM' }))

    expect(screen.getByTestId('video-input')).toHaveTextContent('"reference_role":"voice tone"')
    expect(screen.getByTestId('video-input')).toHaveTextContent('"type":"text"')
    expect(screen.getByTestId('video-input')).toHaveTextContent('声音或 BGM 作为参考')
  })
})
