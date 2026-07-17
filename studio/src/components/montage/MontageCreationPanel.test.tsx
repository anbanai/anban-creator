import { fireEvent, render, screen } from '@testing-library/react'
import { useForm, useWatch } from 'react-hook-form'
import { describe, expect, it, vi } from 'vitest'
import { Form } from '@/components/ui/form'
import type { MontageAsset, MontageInput } from '@/types'
import { MontageCreationPanel } from './MontageCreationPanel'

vi.mock('./MontageSourceAssetInput', () => ({
  MontageSourceAssetInput: ({
    onChange,
    onUploadingChange,
  }: {
    onChange: (value: MontageAsset[]) => void
    onUploadingChange?: (uploading: boolean) => void
  }) => (
    <div>
      <button
        type="button"
        onClick={() => onChange([{ type: 'video_url', url: '/source.mp4', file_name: 'source.mp4' }])}
      >
        添加模拟素材
      </button>
      <button type="button" onClick={() => onUploadingChange?.(true)}>开始模拟上传</button>
      <button type="button" onClick={() => onUploadingChange?.(false)}>结束模拟上传</button>
    </div>
  ),
}))

type PanelFormValues = {
  montage_input: MontageInput
}

function PanelHarness({ onUploadingChange }: { onUploadingChange?: (uploading: boolean) => void }) {
  const form = useForm<PanelFormValues>({
    defaultValues: {
      montage_input: {
        brief: '',
        pipeline_key: '',
        source_assets: [],
        preferences: {
          aspect_ratio: '9:16',
          duration_seconds: 30,
          style: '',
          music_prompt: '',
          subtitle_mode: '',
          voiceover_mode: '',
        },
        delivery_targets: [],
      },
    },
  })
  const input = useWatch({ control: form.control, name: 'montage_input' })

  return (
    <Form {...form}>
      <MontageCreationPanel
        form={form}
        fieldRoot="montage_input"
        onUploadingChange={onUploadingChange}
      />
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
    fireEvent.change(screen.getByLabelText('音乐提示'), {
      target: { value: 'minimal electronic' },
    })
    fireEvent.change(screen.getByLabelText('字幕模式'), {
      target: { value: 'burned-in' },
    })
    fireEvent.change(screen.getByLabelText('配音模式'), {
      target: { value: 'narrated' },
    })
    fireEvent.change(screen.getByPlaceholderText('输入交付目标后按回车'), {
      target: { value: 'final_video' },
    })
    fireEvent.keyDown(screen.getByPlaceholderText('输入交付目标后按回车'), { key: 'Enter' })

    const output = screen.getByTestId('montage-input')
    expect(output).toHaveTextContent('"brief":"新品发布短片"')
    expect(output).toHaveTextContent('"pipeline_key":"social-short"')
    expect(output).toHaveTextContent('"duration_seconds":45')
    expect(output).toHaveTextContent('"music_prompt":"minimal electronic"')
    expect(output).toHaveTextContent('"subtitle_mode":"burned-in"')
    expect(output).toHaveTextContent('"voiceover_mode":"narrated"')
    expect(output).toHaveTextContent('"delivery_targets":["final_video"]')
    expect(screen.queryByText('执行位置')).not.toBeInTheDocument()
    expect(screen.queryByText('高级参数')).not.toBeInTheDocument()
    expect(output).not.toHaveTextContent('execution_target')
    expect(output).not.toHaveTextContent('advanced')
  })

  it('stores source assets and forwards upload state changes', () => {
    const onUploadingChange = vi.fn()
    render(<PanelHarness onUploadingChange={onUploadingChange} />)

    fireEvent.click(screen.getByRole('button', { name: '添加模拟素材' }))
    fireEvent.click(screen.getByRole('button', { name: '开始模拟上传' }))
    fireEvent.click(screen.getByRole('button', { name: '结束模拟上传' }))

    expect(screen.getByTestId('montage-input')).toHaveTextContent(
      '"source_assets":[{"type":"video_url","url":"/source.mp4","file_name":"source.mp4"}]',
    )
    expect(onUploadingChange).toHaveBeenNthCalledWith(1, true)
    expect(onUploadingChange).toHaveBeenNthCalledWith(2, false)
  })
})
