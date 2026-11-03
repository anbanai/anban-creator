import { fireEvent, screen, within } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import DesignerToolbar from './DesignerToolbar'

vi.mock('@/lib/api', () => ({ api: { billing: { wallet: vi.fn().mockResolvedValue({ balance: 1000 }) } } }))

it('offers supported advanced controls on mobile and preserves transparent background', async () => {
  const onSettingsChange = vi.fn()
  const { container } = render(<DesignerToolbar
    designerFeatures={{ qualityLevels: ['auto'], sizePresets: ['1:1:2K'], defaultSize: '1:1:2K', maxBatch: 1, maxReferenceImages: 1, supportsReference: true, supportsMask: true, outputFormats: ['webp'], hasBackground: true, hasCompression: true, watermark: true }}
    settings={{ quality: 'auto', size: '1:1:2K', outputFormat: 'webp', n: 1, compression: 75, background: 'auto', watermark: false }}
    onSettingsChange={onSettingsChange} onHistoryToggle={vi.fn()} />)
  fireEvent.click(screen.getByText('高级参数'))
  const mobileControls = within(container.querySelector('details')!)
  fireEvent.click(mobileControls.getByRole('button', { name: '透明' }))
  expect(onSettingsChange).toHaveBeenCalledWith({ background: 'transparent' })
  expect(mobileControls.getByText('75%')).toBeInTheDocument()
  fireEvent.click(mobileControls.getByRole('button', { name: /水印/ }))
  expect(onSettingsChange).toHaveBeenCalledWith({ watermark: true })
})
