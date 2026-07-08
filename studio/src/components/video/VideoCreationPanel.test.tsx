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
}

const videoProject = {
  id: 'project-1',
  name: '产品视频项目',
} as Project

function PanelHarness() {
  const form = useForm<PanelFormValues>({
    defaultValues: {
      prompt: '',
      video_creator_input: {
        references: [{ type: 'text', text: '保持杯身银色' }],
        hard_constraints: {},
      },
    },
  })
  const videoInput = useWatch({ control: form.control, name: 'video_creator_input' })

  return (
    <Form {...form}>
      <VideoCreationPanel
        form={form}
        fieldRoot="video_creator_input"
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
})
