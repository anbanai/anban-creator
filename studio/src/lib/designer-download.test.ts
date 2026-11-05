import { afterEach, expect, it, vi } from 'vitest'
import { downloadDesignerImage } from './designer-download'

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })
it('downloads the actual image MIME type with its matching extension', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, blob: async () => new Blob(['image'], { type: 'image/webp' }) }))
  vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:download')
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
    expect(this.download).toBe('designer-1.webp')
    expect(this.href).toBe('blob:download')
  })
  await downloadDesignerImage('https://example.com/image', 0)
  expect(click).toHaveBeenCalledOnce()
  vi.unstubAllGlobals()
})
