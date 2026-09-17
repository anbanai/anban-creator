import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useForm, useWatch } from 'react-hook-form'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { Form } from '@/components/ui/form'
import type { ProjectFormValues } from '@/lib/schemas'
import { useMontageCapabilities } from '@/hooks/useMontageCapabilities'
import { MontageProjectDefaultsPanel } from './MontageProjectDefaultsPanel'

vi.mock('@/hooks/useMontageCapabilities', () => ({
  useMontageCapabilities: vi.fn(),
}))

function PanelHarness() {
  const form = useForm<ProjectFormValues>({
    defaultValues: {
      platform: 'montage',
      montage_defaults: {
        default_pipeline: '',
        preferences: {
          duration_seconds: 30,
          style: '',
          music_prompt: '',
          subtitle_mode: '',
          voiceover_mode: '',
        },
        asset_guidance: '',
        delivery_targets: [],
      },
    },
  })
  const defaults = useWatch({ control: form.control, name: 'montage_defaults' })

  return (
    <Form {...form}>
      <MontageProjectDefaultsPanel form={form} />
      <output data-testid="montage-defaults">{JSON.stringify(defaults)}</output>
    </Form>
  )
}

describe('MontageProjectDefaultsPanel', () => {
  beforeEach(() => {
    const items = [
      {
        key: 'cinematic', display_name: '电影感制作', description: '品牌片、预告片与情绪叙事',
        best_for: ['品牌发布'], source_hint: '素材可选', output_hint: '一条完整成片',
        source_requirement: 'optional' as const, output_mode: 'single' as const,
        recommended_duration_seconds: 30,
      },
      {
        key: 'talking-head', display_name: '口播精剪', description: '人物讲解、访谈与课程内容',
        best_for: ['人物口播'], source_hint: '需要视频', output_hint: '一条精剪视频',
        source_requirement: 'video' as const, output_mode: 'single' as const,
        recommended_duration_seconds: 60,
      },
    ]
    vi.mocked(useMontageCapabilities).mockReturnValue({
      items, enabled: true, defaultPipeline: 'cinematic', maxDurationSeconds: 600, maxAssets: 20,
      isLoading: false, isError: false,
      capabilityByKey: (key: string) => items.find((item) => item.key === key),
    })
  })

  it('edits every stable Montage project default', async () => {
    render(<PanelHarness />)

    await waitFor(() => expect(screen.getByRole('radio', { name: /电影感制作/ })).toBeChecked())
    fireEvent.click(screen.getByRole('radio', { name: /口播精剪/ }))
    fireEvent.change(screen.getByLabelText('默认时长（秒）'), { target: { value: '45' } })
    fireEvent.change(screen.getByLabelText('风格偏好'), { target: { value: 'clean documentary' } })
    fireEvent.change(screen.getByLabelText('音乐提示'), { target: { value: 'minimal electronic' } })
    fireEvent.change(screen.getByLabelText('默认字幕'), { target: { value: 'burned_in' } })
    fireEvent.change(screen.getByLabelText('默认配音'), { target: { value: 'narrated' } })
    fireEvent.change(screen.getByLabelText('素材使用说明'), { target: { value: '优先使用实拍素材' } })

    const deliveryInput = screen.getByPlaceholderText('输入交付目标后按回车')
    fireEvent.change(deliveryInput, { target: { value: 'final_video' } })
    fireEvent.keyDown(deliveryInput, { key: 'Enter' })
    fireEvent.change(deliveryInput, { target: { value: 'subtitles' } })
    fireEvent.keyDown(deliveryInput, { key: 'Enter' })

    const output = screen.getByTestId('montage-defaults')
    expect(output).toHaveTextContent('"default_pipeline":"talking-head"')
    expect(screen.queryByLabelText('默认画幅')).not.toBeInTheDocument()
    expect(output).not.toHaveTextContent('aspect_ratio')
    expect(output).toHaveTextContent('"duration_seconds":45')
    expect(output).toHaveTextContent('"style":"clean documentary"')
    expect(output).toHaveTextContent('"music_prompt":"minimal electronic"')
    expect(output).toHaveTextContent('"subtitle_mode":"burned_in"')
    expect(output).toHaveTextContent('"voiceover_mode":"narrated"')
    expect(output).toHaveTextContent('"asset_guidance":"优先使用实拍素材"')
    expect(output).toHaveTextContent('"delivery_targets":["final_video","subtitles"]')
  })

  it('keeps following pipeline recommendations until the default duration is edited', async () => {
    render(<PanelHarness />)

    await waitFor(() => expect(screen.getByRole('radio', { name: /电影感制作/ })).toBeChecked())
    fireEvent.click(screen.getByRole('radio', { name: /口播精剪/ }))
    expect(screen.getByLabelText('默认时长（秒）')).toHaveValue(60)

    fireEvent.click(screen.getByRole('radio', { name: /电影感制作/ }))
    expect(screen.getByLabelText('默认时长（秒）')).toHaveValue(30)

    fireEvent.change(screen.getByLabelText('默认时长（秒）'), { target: { value: '42' } })
    fireEvent.click(screen.getByRole('radio', { name: /口播精剪/ }))
    expect(screen.getByLabelText('默认时长（秒）')).toHaveValue(42)
  })
})
