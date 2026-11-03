import { fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { AxiosError } from 'axios'
import { render } from '@/test/test-utils'
import http from '@/lib/http-client'
import { api } from '@/lib/api'
import { downloadDesignerImage } from '@/lib/designer-download'
import DesignerCanvas from './DesignerCanvas'
import HistoryDrawer from './HistoryDrawer'
import ImagePreview from './ImagePreview'
import InlineMaskEditor from './InlineMaskEditor'

const source = '/api/v1/files/user-1/designer/generation-1/0.png'
const image = { url: source, index: 0 }
const bytes = new Blob(['actual image bytes'], { type: 'image/png' })
const originalAdapter = http.defaults.adapter
let authenticatedReads = 0

beforeEach(() => {
  authenticatedReads = 0
  Object.defineProperty(Element.prototype, 'getAnimations', { configurable: true, value: () => [] })
  localStorage.setItem('anban_creator_token', 'media-test-token')
  vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:authenticated-image')
  http.defaults.adapter = async (config) => {
    if (config.headers.Authorization !== 'Bearer media-test-token') {
      throw new AxiosError('unauthorized', 'ERR_BAD_REQUEST', config, undefined, { status: 401, data: {}, statusText: 'Unauthorized', headers: {}, config })
    }
    expect(config.url).toBe('/files/user-1/designer/generation-1/0.png')
    expect(config.responseType).toBe('blob')
    authenticatedReads++
    return { data: bytes, status: 200, statusText: 'OK', headers: {}, config }
  }
})
afterEach(() => { http.defaults.adapter = originalAdapter; localStorage.clear(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

it('rejects internal file requests without bearer authentication', async () => {
  localStorage.clear()
  await expect(http.get('/files/user-1/designer/generation-1/0.png', { responseType: 'blob' })).rejects.toMatchObject({ response: { status: 401 } })
  expect(authenticatedReads).toBe(0)
})

it('renders authenticated canvas bytes and preserves the durable edit source', async () => {
  const onEdit = vi.fn()
  render(<DesignerCanvas images={[image]} isGenerating={false} canInpaint onImageClick={vi.fn()} onEdit={onEdit} />)
  await waitFor(() => expect(screen.getByAltText('生成图片 1')).toHaveAttribute('src', 'blob:authenticated-image'))
  fireEvent.click(screen.getByTitle('局部编辑'))
  expect(onEdit).toHaveBeenCalledWith(image)
  expect(authenticatedReads).toBe(1)
})

it('renders authenticated history thumbnail bytes', async () => {
  vi.spyOn(api.designer, 'getHistory').mockResolvedValue({ items: [{ id: 'generation-1', project_id: '', prompt: 'saved image', n: 1, status: 'completed', created_at: '', updated_at: '', results: [{ id: 1, generation_id: 'generation-1', image_url: source, index: 0 }] }], total: 1, page: 1, page_size: 50 })
  render(<HistoryDrawer open projectId="" onOpenChange={vi.fn()} onSelect={vi.fn()} />)
  await waitFor(() => expect(document.querySelector('img')).toHaveAttribute('src', 'blob:authenticated-image'))
  expect(authenticatedReads).toBe(1)
})

it('renders authenticated preview bytes and preserves its edit source', async () => {
  const onEdit = vi.fn()
  render(<ImagePreview images={[image]} metadata={{ prompt: 'image', createdAt: '' }} canInpaint onEdit={onEdit} onClose={vi.fn()} />)
  await waitFor(() => expect(screen.getByAltText('预览图片')).toHaveAttribute('src', 'blob:authenticated-image'))
  fireEvent.click(screen.getByRole('button', { name: '局部重绘' }))
  expect(onEdit).toHaveBeenCalledWith(image, 0)
  expect(authenticatedReads).toBe(1)
})

it.each([source, 'https://studio.example.com' + source, 'https://bucket.oss-cn-hangzhou.aliyuncs.com/user-1/designer/generation-1/0.png?Signature=signed'])('loads mask source through the authenticated internal proxy: %s', async (url) => {
  render(<InlineMaskEditor imageUrl={url} onClose={vi.fn()} />)
  await waitFor(() => expect(screen.getByAltText('编辑图片')).toHaveAttribute('src', 'blob:authenticated-image'))
  expect(authenticatedReads).toBe(1)
})

it('downloads the authenticated internal image bytes', async () => {
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 401 }))
  await downloadDesignerImage(source, 0)
  expect(URL.createObjectURL).toHaveBeenCalledWith(bytes)
  expect(click).toHaveBeenCalledOnce()
  expect(authenticatedReads).toBe(1)
})

it('downloads external signed images without sending the Studio JWT', async () => {
  const fetch = vi.fn().mockResolvedValue({ ok: true, blob: async () => bytes })
  vi.stubGlobal('fetch', fetch)
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  const url = 'https://bucket.oss-cn-hangzhou.aliyuncs.com/image.png?Signature=signed'
  await downloadDesignerImage(url, 0)
  expect(fetch).toHaveBeenCalledWith(url)
  expect(authenticatedReads).toBe(0)
})
