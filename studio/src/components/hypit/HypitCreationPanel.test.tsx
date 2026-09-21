import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useForm } from 'react-hook-form'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, it, expect, vi } from 'vitest'
import { Form } from '@/components/ui/form'
import { api } from '@/lib/api'
import { HypitCreationPanel } from './HypitCreationPanel'
vi.mock('@/lib/api', () => ({ api: { hypitCapabilities: { list: vi.fn() } } }))
vi.mock('@/components/ReferenceMaterialInput', () => ({ ReferenceMaterialInput: (props: { uploadPurpose: string; maxFileBytes: number; maxCount: number }) => <div data-testid="upload" data-purpose={props.uploadPurpose} data-max-bytes={props.maxFileBytes} data-max-count={props.maxCount} /> }))
function Harness({ ready }: { ready: (value: boolean) => void }) {
  const form = useForm({ defaultValues: { hypit_input: { brief: 'replace product' } } })
  return <Form {...form}><HypitCreationPanel form={form} onReadyChange={ready} /></Form>
}
function mount(ready: (value: boolean) => void) { render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><Harness ready={ready} /></QueryClientProvider>) }
const capability = { enabled: true, configured: true, missing_configuration: [], limits: { max_duration_seconds: 60, max_assets: 3, max_asset_bytes: 12345, max_input_bytes: 50000 } }
describe('video replication creation panel', () => {
  it('gates readiness on reference and propagates server upload limits', async () => {
    vi.mocked(api.hypitCapabilities.list).mockResolvedValue(capability)
    const ready = vi.fn(); mount(ready)
    await screen.findByText('请上传参考视频或填写参考视频链接')
    expect(ready).toHaveBeenLastCalledWith(false)
    fireEvent.change(screen.getByLabelText('参考视频链接'), { target: { value: 'https://example.com/ref.mp4' } })
    await waitFor(() => expect(ready).toHaveBeenLastCalledWith(true))
    expect(screen.getAllByTestId('upload')[0]).toHaveAttribute('data-purpose', 'hypit_asset')
    expect(screen.getAllByTestId('upload')[0]).toHaveAttribute('data-max-bytes', '12345')
    expect(screen.getAllByTestId('upload')[1]).toHaveAttribute('data-max-count', '3')
  })
  it('explains disabled capability and prevents submission', async () => {
    vi.mocked(api.hypitCapabilities.list).mockResolvedValue({ ...capability, enabled: false })
    const ready = vi.fn(); mount(ready)
    await screen.findByText('视频复刻尚未启用，请联系管理员开启内部验证。')
    expect(ready).toHaveBeenLastCalledWith(false)
  })
})
