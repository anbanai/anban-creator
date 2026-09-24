import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useForm } from 'react-hook-form'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, it, expect, vi } from 'vitest'
import { Form } from '@/components/ui/form'
import { api } from '@/lib/api'
import { HypitCreationPanel } from './HypitCreationPanel'

vi.mock('@/lib/api', () => ({ api: { hypitCapabilities: { list: vi.fn() } } }))
vi.mock('@/components/ReferenceMaterialInput', () => ({ ReferenceMaterialInput: (props: { uploadPurpose: string; maxFileBytes: number; maxCount: number; allowedTypes: string[] }) => <div data-testid="upload" data-purpose={props.uploadPurpose} data-max-bytes={props.maxFileBytes} data-max-count={props.maxCount} data-types={props.allowedTypes.join(',')} /> }))

function Harness({ ready, sharedBrief = false, defaults = false, remix = false, submit }: {
  ready?: (value: boolean) => void
  sharedBrief?: boolean
  defaults?: boolean
  remix?: boolean
  submit?: (value: unknown) => void
}) {
  const form = useForm({ defaultValues: { hypit_input: { brief: 'replace product', preferences: {} }, hypit_defaults: { preferences: {}, asset_guidance: '' } } })
  return <Form {...form}><form onSubmit={form.handleSubmit(value => submit?.(value))}>
    <HypitCreationPanel form={form} defaults={defaults} remix={remix} onReadyChange={ready} briefField={sharedBrief ? <textarea aria-label="复刻要求" {...form.register('hypit_input.brief')} /> : undefined} />
    <button type="submit">保存</button>
  </form></Form>
}
function mount(props: Parameters<typeof Harness>[0] = {}) {
  return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><Harness {...props} /></QueryClientProvider>)
}
const capability = { enabled: true, configured: true, missing_configuration: [], limits: { max_duration_seconds: 60, max_assets: 3, max_asset_bytes: 12345, max_input_bytes: 50000 } }

describe('video replication creation panel', () => {
  it('uses a single shared brief and allows clearing the optional duration', async () => {
    vi.mocked(api.hypitCapabilities.list).mockResolvedValue(capability)
    const ready = vi.fn()
    const submit = vi.fn()
    mount({ ready, sharedBrief: true, submit })
    await screen.findByText('请上传主参考视频或填写主参考视频链接')
    expect(screen.getAllByLabelText('复刻要求')).toHaveLength(1)
    expect(screen.getByText('主参考视频')).toBeInTheDocument()
    const duration = screen.getByLabelText('目标时长（可选）')
    expect(duration).toHaveAttribute('placeholder', '跟随参考视频')
    expect(duration).toHaveValue(null)
    fireEvent.change(screen.getByLabelText('主参考视频链接'), { target: { value: 'https://example.com/ref.mp4' } })
    await waitFor(() => expect(ready).toHaveBeenLastCalledWith(true))
    fireEvent.change(duration, { target: { value: '61' } })
    await screen.findByText('时长不能超过 60 秒')
    expect(ready).toHaveBeenLastCalledWith(false)
    fireEvent.change(duration, { target: { value: '' } })
    await waitFor(() => expect(ready).toHaveBeenLastCalledWith(true))
    fireEvent.click(screen.getByText('保存'))
    await waitFor(() => expect(submit).toHaveBeenCalled())
    expect(submit.mock.calls[0][0].hypit_input.preferences.duration_seconds).toBeUndefined()
  })
  it('limits the primary reference to one video and permits all supplemental media types', async () => {
    vi.mocked(api.hypitCapabilities.list).mockResolvedValue(capability)
    const ready = vi.fn(); mount({ ready })
    await screen.findByText('请上传主参考视频或填写主参考视频链接')
    expect(ready).toHaveBeenLastCalledWith(false)
    fireEvent.change(screen.getByLabelText('主参考视频链接'), { target: { value: 'https://example.com/ref.mp4' } })
    await waitFor(() => expect(ready).toHaveBeenLastCalledWith(true))
    const uploads = screen.getAllByTestId('upload')
    expect(uploads[0]).toHaveAttribute('data-purpose', 'hypit_asset')
    expect(uploads[0]).toHaveAttribute('data-max-bytes', '12345')
    expect(uploads[0]).toHaveAttribute('data-max-count', '1')
    expect(uploads[0]).toHaveAttribute('data-types', 'video')
    expect(uploads[1]).toHaveAttribute('data-max-count', '3')
    expect(uploads[1]).toHaveAttribute('data-types', 'image,video,audio')
  })
  it('allows project defaults without a duration and retains separate asset guidance', async () => {
    vi.mocked(api.hypitCapabilities.list).mockResolvedValue(capability)
    const ready = vi.fn(); mount({ ready, defaults: true })
    await waitFor(() => expect(ready).toHaveBeenLastCalledWith(true))
    expect(screen.getByLabelText('默认时长（可选）')).toHaveValue(null)
    expect(screen.getByLabelText('素材使用说明')).toBeInTheDocument()
    expect(screen.queryByLabelText('复刻要求')).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('默认时长（可选）'), { target: { value: '61' } })
    await waitFor(() => expect(ready).toHaveBeenLastCalledWith(false))
  })
  it('allows remix to reuse the original project without a new reference', async () => {
    vi.mocked(api.hypitCapabilities.list).mockResolvedValue(capability)
    const ready = vi.fn(); mount({ ready, remix: true })
    await waitFor(() => expect(ready).toHaveBeenLastCalledWith(true))
    expect(screen.getByText('主参考视频（可选，沿用原工程）')).toBeInTheDocument()
  })
  it('explains disabled capability and prevents submission', async () => {
    vi.mocked(api.hypitCapabilities.list).mockResolvedValue({ ...capability, enabled: false })
    const ready = vi.fn(); mount({ ready })
    await screen.findByText('视频复刻尚未启用，请联系管理员开启内部验证。')
    expect(ready).toHaveBeenLastCalledWith(false)
  })
})
