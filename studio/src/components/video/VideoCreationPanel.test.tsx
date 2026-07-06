import { fireEvent, render, screen } from '@testing-library/react'
import { useWatch, useForm } from 'react-hook-form'
import { describe, expect, it } from 'vitest'
import { Form, FormControl, FormField, FormItem } from '@/components/ui/form'
import { Textarea } from '@/components/ui/textarea'
import type { Project, VideoTaskConfig } from '@/types'
import { VideoCreationPanel } from './VideoCreationPanel'

type PanelFormValues = {
  prompt: string
  video_config: VideoTaskConfig
}

const videoProject = {
  id: 'project-1',
  name: '产品视频项目',
  video_defaults: {
    purpose: 'planting',
    model_key: 'seedance-2.0-mini',
    resolution: '720p',
    ratio: '9:16',
    duration: 15,
    watermark: false,
  },
} as Project

function PanelHarness() {
  const form = useForm<PanelFormValues>({
    defaultValues: {
      prompt: '',
      video_config: {
        model_key: 'custom-video-model',
        references: [{ type: 'text', text: '保持杯身银色', reference_role: 'subject identity' }],
      },
    },
  })
  const videoConfig = useWatch({ control: form.control, name: 'video_config' })

  return (
    <Form {...form}>
      <VideoCreationPanel
        form={form}
        selectedProject={videoProject}
        availableVideoModels={[]}
        playbooks={[
          {
            key: 'live_selling',
            label: '直播带货',
            creative_type: 'product_demo',
            purpose: 'ecommerce',
            required_reference_roles: ['product appearance', 'action'],
            default_ratio: '9:16',
            prompt_scaffold: '黄金三秒开场、一个核心卖点、亲手展示。',
            qc_focus: ['产品保真', 'CTA'],
            risk_notes: ['不要编造优惠。'],
          },
          {
            key: 'brand_promo',
            label: '品牌宣传',
            creative_type: 'brand_promo',
            purpose: 'promotion',
            required_reference_roles: ['scene background'],
            default_ratio: '16:9',
            prompt_scaffold: '只保留一个品牌记忆点。',
            qc_focus: ['品牌记忆'],
            risk_notes: ['文字建议后期加。'],
          },
        ]}
        minimumBalanceHint="视频任务需至少 100,000 积分余额。"
        promptField={(
          <FormField control={form.control} name="prompt" render={({ field }) => (
            <FormItem>
              <FormControl>
                <Textarea {...field} />
              </FormControl>
            </FormItem>
          )} />
        )}
        estimateSummary={<p>估算摘要</p>}
      />
      <output data-testid="video-config">{JSON.stringify(videoConfig)}</output>
    </Form>
  )
}

describe('VideoCreationPanel', () => {
  it('keeps advanced settings controlled and restores project defaults without clearing references', () => {
    render(<PanelHarness />)

    expect(screen.queryByText('没有可用视频模型，请先在项目策略中选择已配置模型。')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '高级设置' }))
    expect(screen.getByText('没有可用视频模型，请先在项目策略中选择已配置模型。')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '恢复项目默认' }))

    expect(screen.getByTestId('video-config')).toHaveTextContent('seedance-2.0-mini')
    expect(screen.getByTestId('video-config')).toHaveTextContent('保持杯身银色')
  })

  it('lets the operator choose the editor workflow', () => {
    render(<PanelHarness />)

    fireEvent.click(screen.getByRole('button', { name: '剪辑' }))

    expect(screen.getByTestId('video-config')).toHaveTextContent('"workflow":"editor"')
    expect(screen.getByText('剪辑要求')).toBeInTheDocument()
  })

  it('applies a playbook and production mode before detailed prompting', () => {
    render(<PanelHarness />)

    fireEvent.click(screen.getByRole('button', { name: /直播带货/ }))
    fireEvent.click(screen.getByRole('button', { name: '专业序列' }))

    expect(screen.getByTestId('video-config')).toHaveTextContent('"scenario_key":"live_selling"')
    expect(screen.getByTestId('video-config')).toHaveTextContent('"creative_type":"product_demo"')
    expect(screen.getByTestId('video-config')).toHaveTextContent('"purpose":"ecommerce"')
    expect(screen.getByTestId('video-config')).toHaveTextContent('"ratio":"9:16"')
    expect(screen.getByTestId('video-config')).toHaveTextContent('"production_mode":"sequence"')
  })
})
