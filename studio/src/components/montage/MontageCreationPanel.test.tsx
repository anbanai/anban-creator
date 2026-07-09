import { fireEvent, render, screen } from '@testing-library/react'
import { useForm, useWatch } from 'react-hook-form'
import { describe, expect, it } from 'vitest'
import { Form } from '@/components/ui/form'
import type { MontageInput } from '@/types'
import { MontageCreationPanel } from './MontageCreationPanel'

type PanelFormValues = {
  montage_input: MontageInput
}

function PanelHarness() {
  const form = useForm<PanelFormValues>({
    defaultValues: {
      montage_input: {
        brief: '',
        pipeline_key: '',
        preferences: {
          aspect_ratio: '9:16',
          duration_seconds: 30,
          style: '',
        },
      },
    },
  })
  const input = useWatch({ control: form.control, name: 'montage_input' })

  return (
    <Form {...form}>
      <MontageCreationPanel form={form} fieldRoot="montage_input" />
      <output data-testid="montage-input">{JSON.stringify(input)}</output>
    </Form>
  )
}

describe('MontageCreationPanel', () => {
  it('edits stable montage fields without execution target controls', () => {
    render(<PanelHarness />)

    fireEvent.change(screen.getByPlaceholderText('描述这次要生产的视频内容、素材用途、节奏和交付目标'), {
      target: { value: '新品发布短片' },
    })
    fireEvent.change(screen.getByPlaceholderText('pipeline（可选）'), {
      target: { value: 'social-short' },
    })
    fireEvent.change(screen.getByLabelText('时长（秒）'), {
      target: { value: '45' },
    })

    expect(screen.getByTestId('montage-input')).toHaveTextContent('"brief":"新品发布短片"')
    expect(screen.getByTestId('montage-input')).toHaveTextContent('"pipeline_key":"social-short"')
    expect(screen.getByTestId('montage-input')).toHaveTextContent('"duration_seconds":45')
    expect(screen.queryByText('执行位置')).not.toBeInTheDocument()
    expect(screen.getByTestId('montage-input')).not.toHaveTextContent('execution_target')
  })
})
