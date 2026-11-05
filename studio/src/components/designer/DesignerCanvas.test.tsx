import { fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import DesignerCanvas from './DesignerCanvas'

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })
it('opens the selected result, edits it and downloads its actual image format', async () => {
  const onImageClick = vi.fn()
  const onEdit = vi.fn()
  const image = { url: 'https://example.com/result', index: 0 }
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, blob: async () => new Blob(['result'], { type: 'image/jpeg' }) }))
  vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:result')
  const downloaded: string[] = []
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) { downloaded.push(this.download) })
  render(<DesignerCanvas images={[image]} isGenerating={false} canInpaint onImageClick={onImageClick} onEdit={onEdit} />)
  fireEvent.click(screen.getByTitle('全屏查看'))
  expect(onImageClick).toHaveBeenCalledWith(image)
  fireEvent.click(screen.getByTitle('局部编辑'))
  expect(onEdit).toHaveBeenCalledWith(image)
  fireEvent.click(screen.getByTitle('下载'))
  await waitFor(() => expect(downloaded).toEqual(['designer-1.jpg']))
})
