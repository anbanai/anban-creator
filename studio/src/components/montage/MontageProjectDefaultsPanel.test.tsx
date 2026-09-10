import { fireEvent, render, screen } from '@testing-library/react'
import { useForm, useWatch } from 'react-hook-form'
import { describe, expect, it } from 'vitest'
import { Form } from '@/components/ui/form'
import type { ProjectFormValues } from '@/lib/schemas'
import { MontageProjectDefaultsPanel } from './MontageProjectDefaultsPanel'

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
  it('edits every stable Montage project default', () => {
    render(<PanelHarness />)

    fireEvent.change(screen.getByLabelText('默认 Pipeline'), { target: { value: 'social-short' } })
    fireEvent.change(screen.getByLabelText('默认时长（秒）'), { target: { value: '45' } })
    fireEvent.change(screen.getByLabelText('风格偏好'), { target: { value: 'clean documentary' } })
    fireEvent.change(screen.getByLabelText('音乐提示'), { target: { value: 'minimal electronic' } })
    fireEvent.change(screen.getByLabelText('字幕模式'), { target: { value: 'burned-in' } })
    fireEvent.change(screen.getByLabelText('配音模式'), { target: { value: 'narrated' } })
    fireEvent.change(screen.getByLabelText('素材使用说明'), { target: { value: '优先使用实拍素材' } })

    const deliveryInput = screen.getByPlaceholderText('输入交付目标后按回车')
    fireEvent.change(deliveryInput, { target: { value: 'final_video' } })
    fireEvent.keyDown(deliveryInput, { key: 'Enter' })
    fireEvent.change(deliveryInput, { target: { value: 'subtitles' } })
    fireEvent.keyDown(deliveryInput, { key: 'Enter' })

    const output = screen.getByTestId('montage-defaults')
    expect(output).toHaveTextContent('"default_pipeline":"social-short"')
    expect(screen.queryByLabelText('默认画幅')).not.toBeInTheDocument()
    expect(output).not.toHaveTextContent('aspect_ratio')
    expect(output).toHaveTextContent('"duration_seconds":45')
    expect(output).toHaveTextContent('"style":"clean documentary"')
    expect(output).toHaveTextContent('"music_prompt":"minimal electronic"')
    expect(output).toHaveTextContent('"subtitle_mode":"burned-in"')
    expect(output).toHaveTextContent('"voiceover_mode":"narrated"')
    expect(output).toHaveTextContent('"asset_guidance":"优先使用实拍素材"')
    expect(output).toHaveTextContent('"delivery_targets":["final_video","subtitles"]')
  })
})
