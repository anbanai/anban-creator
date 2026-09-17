import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useForm, useWatch } from 'react-hook-form'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { Form } from '@/components/ui/form'
import type { MontageAsset, MontageInput, MontagePipelineCapability } from '@/types'
import { useMontageCapabilities } from '@/hooks/useMontageCapabilities'
import { MontageCreationPanel } from './MontageCreationPanel'

const capabilities: MontagePipelineCapability[] = [
  {
    key: 'cinematic', display_name: '电影感制作', description: '品牌片、预告片与情绪叙事',
    best_for: ['品牌发布', '概念预告'], source_hint: '可使用视频、图片，也可仅根据创意说明生成',
    output_hint: '一条完整成片', source_requirement: 'optional', output_mode: 'single',
    recommended_duration_seconds: 30,
  },
  {
    key: 'talking-head', display_name: '口播精剪', description: '人物讲解、访谈与课程内容',
    best_for: ['人物口播', '采访精剪'], source_hint: '需要一段包含人物讲话的原始视频',
    output_hint: '一条带字幕的精剪视频', source_requirement: 'video', output_mode: 'single',
    recommended_duration_seconds: 60,
  },
  {
    key: 'screen-demo', display_name: '屏幕演示', description: '产品教程、软件操作与终端流程',
    best_for: ['产品演示', '终端操作'], source_hint: '可上传屏幕录制，或在说明中给出可复现的操作步骤',
    output_hint: '一条清晰的演示视频', source_requirement: 'optional', output_mode: 'single',
    recommended_duration_seconds: 60,
  },
  {
    key: 'clip-factory', display_name: '长视频拆条', description: '从直播、播客或访谈中提炼短视频',
    best_for: ['直播切片', '播客拆条'], source_hint: '需要一段长视频或音频作为拆条来源',
    output_hint: '多条可独立发布的短视频', source_requirement: 'video_or_audio', output_mode: 'multiple',
    recommended_duration_seconds: 45,
  },
]

vi.mock('@/hooks/useMontageCapabilities', () => ({
  useMontageCapabilities: vi.fn(),
}))

vi.mock('./MontageSourceAssetInput', () => ({
  MontageSourceAssetInput: ({
    value,
    onChange,
    onUploadingChange,
    hint,
    maxCount,
  }: {
    value: MontageAsset[]
    onChange: (value: MontageAsset[]) => void
    onUploadingChange?: (uploading: boolean) => void
    hint?: string
    maxCount?: number
  }) => (
    <div>
      <p>{hint}</p>
      <p>{value.length}/{maxCount}</p>
      <button
        type="button"
        onClick={() => onChange([{ type: 'video_url', url: '/source.mp4', file_name: 'source.mp4' }])}
      >
        添加模拟视频
      </button>
      <button type="button" onClick={() => onUploadingChange?.(true)}>开始模拟上传</button>
      <button type="button" onClick={() => onUploadingChange?.(false)}>结束模拟上传</button>
    </div>
  ),
}))

type PanelFormValues = {
  montage_input: MontageInput
}

function PanelHarness({
  input,
  onUploadingChange,
  onReadyChange,
}: {
  input?: MontageInput
  onUploadingChange?: (uploading: boolean) => void
  onReadyChange?: (ready: boolean) => void
}) {
  const form = useForm<PanelFormValues>({
    defaultValues: {
      montage_input: input ?? {
        brief: '', pipeline_key: '', source_assets: [],
        preferences: { style: '', music_prompt: '', subtitle_mode: '', voiceover_mode: '' },
        delivery_targets: [],
      },
    },
  })
  const value = useWatch({ control: form.control, name: 'montage_input' })

  return (
    <Form {...form}>
      <MontageCreationPanel
        form={form}
        fieldRoot="montage_input"
        onUploadingChange={onUploadingChange}
        onReadyChange={onReadyChange}
      />
      <output data-testid="montage-input">{JSON.stringify(value)}</output>
    </Form>
  )
}

describe('MontageCreationPanel', () => {
  beforeEach(() => {
    vi.mocked(useMontageCapabilities).mockReturnValue({
      items: capabilities, enabled: true, defaultPipeline: 'cinematic',
      maxDurationSeconds: 600, maxAssets: 20, isLoading: false, isError: false,
      capabilityByKey: (key: string) => capabilities.find((item) => item.key === key),
    })
  })

  it('fills the untouched catalog default and selects a video type with dynamic output copy', async () => {
    render(<PanelHarness />)

    await waitFor(() => expect(screen.getByRole('radio', { name: /电影感制作/ })).toBeChecked())
    expect(screen.getByTestId('montage-input')).toHaveTextContent('"pipeline_key":"cinematic"')
    expect(screen.getByTestId('montage-input')).toHaveTextContent('"duration_seconds":30')

    fireEvent.click(screen.getByRole('radio', { name: /长视频拆条/ }))

    expect(screen.getByRole('radio', { name: /长视频拆条/ })).toBeChecked()
    expect(screen.getByText('单条目标时长')).toBeInTheDocument()
    expect(screen.getByText('多条可独立发布的短视频')).toBeInTheDocument()
    expect(screen.getByTestId('montage-input')).toHaveTextContent('"pipeline_key":"clip-factory"')
    expect(screen.getByTestId('montage-input')).toHaveTextContent('"duration_seconds":45')
  })

  it('keeps following pipeline recommendations until the user edits the duration', async () => {
    render(<PanelHarness />)

    await waitFor(() => expect(screen.getByLabelText('目标时长（秒）')).toHaveValue(30))
    fireEvent.click(screen.getByRole('radio', { name: /口播精剪/ }))
    expect(screen.getByLabelText('目标时长（秒）')).toHaveValue(60)

    fireEvent.click(screen.getByRole('radio', { name: /长视频拆条/ }))
    expect(screen.getByLabelText('单条目标时长（秒）')).toHaveValue(45)

    fireEvent.change(screen.getByLabelText('单条目标时长（秒）'), { target: { value: '52' } })
    fireEvent.click(screen.getByRole('radio', { name: /电影感制作/ }))
    expect(screen.getByLabelText('目标时长（秒）')).toHaveValue(52)
  })

  it('shows the selected source requirement inline and becomes ready after a valid upload', async () => {
    const onReadyChange = vi.fn()
    render(<PanelHarness onReadyChange={onReadyChange} />)

	await waitFor(() => expect(screen.getByRole('radio', { name: /电影感制作/ })).toBeChecked())
    fireEvent.click(screen.getByRole('radio', { name: /口播精剪/ }))
    expect(screen.getByText('请添加至少一段视频素材')).toBeInTheDocument()
	expect(screen.getAllByText('需要一段包含人物讲话的原始视频')).not.toHaveLength(0)
	await waitFor(() => expect(onReadyChange).toHaveBeenLastCalledWith(false))

    fireEvent.click(screen.getByRole('button', { name: '添加模拟视频' }))

    await waitFor(() => expect(screen.queryByText('请添加至少一段视频素材')).not.toBeInTheDocument())
    expect(onReadyChange).toHaveBeenLastCalledWith(true)
  })

  it('accepts an existing video asset alias with a usable locator', async () => {
    const onReadyChange = vi.fn()
    render(<PanelHarness input={{
      brief: '历史口播', pipeline_key: 'talking-head',
      source_assets: [{ type: 'video', task_file_id: 'task-file-1' }],
      preferences: { duration_seconds: 60 }, delivery_targets: [],
    }} onReadyChange={onReadyChange} />)

    expect(screen.queryByText('请添加至少一段视频素材')).not.toBeInTheDocument()
    await waitFor(() => expect(onReadyChange).toHaveBeenLastCalledWith(true))
  })

  it('keeps required media invalid when its locator is empty', async () => {
    const onReadyChange = vi.fn()
    render(<PanelHarness input={{
      brief: '空素材占位', pipeline_key: 'talking-head',
      source_assets: [{ type: 'video_url' }],
      preferences: { duration_seconds: 60 }, delivery_targets: [],
    }} onReadyChange={onReadyChange} />)

    expect(screen.getByText('请添加至少一段视频素材')).toBeInTheDocument()
    await waitFor(() => expect(onReadyChange).toHaveBeenLastCalledWith(false))
  })

  it('serializes product subtitle and voice choices and keeps advanced requirements collapsed', async () => {
    render(<PanelHarness />)
    await waitFor(() => expect(screen.getByRole('radio', { name: /电影感制作/ })).toBeChecked())

    expect(screen.queryByLabelText('风格偏好')).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('字幕'), { target: { value: 'burned_in' } })
    fireEvent.change(screen.getByLabelText('配音'), { target: { value: 'narrated' } })
    fireEvent.click(screen.getByRole('button', { name: /更多创作要求/ }))
    fireEvent.change(screen.getByLabelText('风格偏好'), { target: { value: '干净克制' } })

    const output = screen.getByTestId('montage-input')
    expect(output).toHaveTextContent('"subtitle_mode":"burned_in"')
    expect(output).toHaveTextContent('"voiceover_mode":"narrated"')
    expect(output).toHaveTextContent('"style":"干净克制"')
    expect(screen.queryByText('执行位置')).not.toBeInTheDocument()
    expect(output).not.toHaveTextContent('execution_target')
  })

  it('preserves historical custom subtitle and voice values while editing', () => {
    render(<PanelHarness input={{
      brief: '历史视频', pipeline_key: 'cinematic', source_assets: [],
      preferences: {
        duration_seconds: 25,
        subtitle_mode: 'legacy-caption-track',
        voiceover_mode: 'studio-voice-v2',
      },
      delivery_targets: [],
    }} />)

    expect(screen.getByLabelText('字幕')).toHaveValue('legacy-caption-track')
    expect(screen.getByRole('option', { name: '已有自定义值：legacy-caption-track' })).toBeInTheDocument()
    expect(screen.getByLabelText('配音')).toHaveValue('studio-voice-v2')
    expect(screen.getByRole('option', { name: '已有自定义值：studio-voice-v2' })).toBeInTheDocument()
    expect(screen.getByTestId('montage-input')).toHaveTextContent('"subtitle_mode":"legacy-caption-track"')
    expect(screen.getByTestId('montage-input')).toHaveTextContent('"voiceover_mode":"studio-voice-v2"')
  })

  it('forwards upload state changes', () => {
    const onUploadingChange = vi.fn()
    render(<PanelHarness onUploadingChange={onUploadingChange} />)

    fireEvent.click(screen.getByRole('button', { name: '开始模拟上传' }))
    fireEvent.click(screen.getByRole('button', { name: '结束模拟上传' }))

    expect(onUploadingChange).toHaveBeenNthCalledWith(1, true)
    expect(onUploadingChange).toHaveBeenNthCalledWith(2, false)
  })

  it('blocks readiness when the target duration exceeds the catalog limit', async () => {
    const onReadyChange = vi.fn()
    render(<PanelHarness onReadyChange={onReadyChange} />)

    await waitFor(() => expect(screen.getByRole('radio', { name: /电影感制作/ })).toBeChecked())
    fireEvent.change(screen.getByLabelText('目标时长（秒）'), { target: { value: '601' } })

    expect(screen.getByText('目标时长不能超过 600 秒')).toBeInTheDocument()
    await waitFor(() => expect(onReadyChange).toHaveBeenLastCalledWith(false))
  })

  it('does not overwrite an existing pipeline or duration when the catalog loads later', async () => {
    vi.mocked(useMontageCapabilities).mockReturnValue({
      items: [], enabled: false, defaultPipeline: '',
      maxDurationSeconds: 0, maxAssets: 0, isLoading: true, isError: false,
      capabilityByKey: () => undefined,
    })
    const input: MontageInput = {
      brief: '项目快照', pipeline_key: 'screen-demo', source_assets: [],
      preferences: { duration_seconds: 75 }, delivery_targets: [],
    }
    const { rerender } = render(<PanelHarness input={input} />)

    vi.mocked(useMontageCapabilities).mockReturnValue({
      items: capabilities, enabled: true, defaultPipeline: 'cinematic',
      maxDurationSeconds: 600, maxAssets: 20, isLoading: false, isError: false,
      capabilityByKey: (key: string) => capabilities.find((item) => item.key === key),
    })
    rerender(<PanelHarness input={input} />)

    expect(screen.getByRole('radio', { name: /屏幕演示/ })).toBeChecked()
    expect(screen.getByLabelText('目标时长（秒）')).toHaveValue(75)
    expect(screen.getByTestId('montage-input')).toHaveTextContent('"pipeline_key":"screen-demo"')
  })
})
