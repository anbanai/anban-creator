import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { http, HttpResponse } from 'msw'
import { server } from '@/test/mocks/server'
import PublicSharePage from './PublicSharePage'

const content = { type: 'seednote', title: '周末的公园', format: 'markdown', body: '开篇\n\n' + '完整内容。'.repeat(600) + '\n\n最后一段', images: [], expires_at: '2030-11-04T12:00:00Z' }
function open() {
  return render(<MemoryRouter initialEntries={['/share/test-token']}><Routes><Route path="/share/:token" element={<PublicSharePage />} /></Routes></MemoryRouter>)
}
afterEach(() => { localStorage.clear(); vi.unstubAllEnvs(); vi.restoreAllMocks() })

describe('PublicSharePage', () => {
  it.each([false, true])('reads complete content anonymously with stored login=%s and restores metadata', async (loggedIn) => {
    if (loggedIn) localStorage.setItem('anban_creator_token', 'expired-private-token')
    let auth: string | null = 'unset'
    const fetchRequest = vi.spyOn(globalThis, 'fetch')
    server.use(http.get('/api/v1/public/shares/test-token', ({ request }) => {
      auth = request.headers.get('authorization')
      return HttpResponse.json({ data: content })
    }))
    document.title = '工作台'
    const view = open()
    await screen.findByRole('heading', { name: '周末的公园' })
    expect(screen.getByText('最后一段')).toBeInTheDocument()
    expect(screen.getByText(/完整内容/).textContent?.length).toBe(3000)
    expect(auth).toBeNull()
    expect(fetchRequest).toHaveBeenCalledWith('http://localhost:3000/api/v1/public/shares/test-token', expect.objectContaining({ cache: 'no-store', credentials: 'omit', referrerPolicy: 'no-referrer' }))
    expect(document.querySelector('meta[name="robots"]')).toHaveAttribute('content', 'noindex, nofollow')
    expect(document.querySelector('meta[name="referrer"]')).toHaveAttribute('content', 'no-referrer')
    view.unmount()
    expect(document.title).toBe('工作台')
  })

  it.each([404, 410, 409, 429, 500])('shows safe error state for HTTP %s', async (status) => {
    server.use(http.get('/api/v1/public/shares/test-token', () => new HttpResponse(null, { status })))
    open()
    expect(await screen.findByRole('heading', { name: status === 404 || status === 410 ? '分享链接已失效' : status === 409 ? '内容暂不可用' : '暂时无法加载' })).toBeInTheDocument()
    if (status !== 404 && status !== 410) expect(screen.getByRole('button', { name: '重新加载' })).toBeInTheDocument()
  })

  it('rejects stale content and refetches on revisit', async () => {
    let calls = 0
    server.use(http.get('/api/v1/public/shares/test-token', () => { calls++; return HttpResponse.json({ data: { ...content, expires_at: calls === 1 ? '2000-01-01T00:00:00Z' : content.expires_at } }) }))
    const first = open()
    await screen.findByText('分享链接已失效')
    expect(screen.queryByText('最后一段')).not.toBeInTheDocument()
    first.unmount()
    open()
    await screen.findByRole('heading', { name: content.title })
    expect(calls).toBe(2)
  })

  it('keeps Markdown gallery order, avoids body duplicates and removes raw HTML/external images', async () => {
    server.use(http.get('/api/v1/public/shares/test-token', () => HttpResponse.json({ data: { ...content,
      body: '![正文](/api/v1/public/shares/test-token/images/two)\n\n<img src="https://tracker.test/pixel">\n\n![外部](https://tracker.test/leak)',
      images: [{ id: 'one', role: 'cover', alt: '封面', url: '/api/v1/public/shares/test-token/images/one' }, { id: 'two', role: 'image', alt: '正文', url: '/api/v1/public/shares/test-token/images/two' }, { id: 'three', role: 'tail', alt: '尾图', url: '/api/v1/public/shares/test-token/images/three' }],
    } })))
    open()
    await screen.findByRole('heading', { name: content.title })
    const images = screen.getAllByRole('img')
    expect(images.map((image) => image.getAttribute('alt'))).toEqual(['正文', '封面', '尾图'])
    expect(images.every((image) => !image.getAttribute('src')?.includes('tracker.test'))).toBe(true)
    fireEvent.error(images[0])
    await screen.findByText('图片加载失败')
    fireEvent.click(screen.getByRole('button', { name: '重新加载' }))
    await screen.findByRole('heading', { name: content.title })
  })

  it('renders HTML only inside a scriptless isolated frame and reports image request errors', async () => {
    server.use(http.get('/api/v1/public/shares/test-token', () => HttpResponse.json({ data: { ...content, type: 'wechat-article', format: 'html', body: '<p>完整文章</p><script>alert(1)</script><img src="https://tracker.test/pixel">' } })))
    open()
    const frame = await screen.findByTitle(content.title)
    expect(frame).toHaveAttribute('sandbox', '')
    expect(frame.getAttribute('srcdoc')).toContain('完整文章')
    expect(frame.getAttribute('srcdoc')).not.toContain('alert(1)')
    expect(frame.getAttribute('srcdoc')).not.toContain('tracker.test')
    expect(frame.getAttribute('srcdoc')).toContain("default-src 'none'")
  })

  it('resolves images against the configured API origin and rejects unauthorized external images', async () => {
    vi.stubEnv('VITE_API_BASE_URL', 'https://api.anban.test/api/v1')
    server.use(http.get('https://api.anban.test/api/v1/public/shares/test-token', () => HttpResponse.json({ data: { ...content, images: [{ id: 'one', role: 'cover', alt: '封面', url: '/api/v1/public/shares/test-token/images/one' }] } })))
    open()
    const image = await screen.findByRole('img', { name: '封面' })
    expect(image).toHaveAttribute('src', 'https://api.anban.test/api/v1/public/shares/test-token/images/one')
  })

  it('loads and decodes HTML raster data URLs before displaying the opaque frame, with retry for missing bytes', async () => {
    let imageAttempts = 0
    const fetchRequest = vi.spyOn(globalThis, 'fetch')
    server.use(
      http.get('/api/v1/public/shares/test-token', () => HttpResponse.json({ data: { ...content, format: 'html', body: '<img src="/api/v1/public/shares/test-token/images/one">', images: [{ id: 'one', role: 'cover', alt: '封面', url: '/api/v1/public/shares/test-token/images/one' }] } })),
      http.get('/api/v1/public/shares/test-token/images/one', ({ request }) => {
        expect(request.headers.get('authorization')).toBeNull()
        imageAttempts++
        return imageAttempts === 1 ? new HttpResponse(null, { status: 500 }) : new HttpResponse(new Uint8Array([137, 80, 78, 71]), { headers: { 'Content-Type': 'image/png' } })
      }),
    )
    open()
    await screen.findByText('内容暂不可用')
    fireEvent.click(screen.getByRole('button', { name: '重新加载' }))
    await waitFor(() => expect(document.querySelector('img')).not.toBeNull())
    expect(screen.queryByTitle(content.title)).not.toBeInTheDocument()
    expect(document.querySelector('img')).toHaveAttribute('src', 'data:image/png;base64,iVBORw==')
    fireEvent.load(document.querySelector('img')!)
    const frame = await screen.findByTitle(content.title)
    expect(fetchRequest).toHaveBeenCalledWith('http://localhost:3000/api/v1/public/shares/test-token/images/one', expect.objectContaining({ cache: 'no-store', credentials: 'omit', referrerPolicy: 'no-referrer' }))
    expect(frame.getAttribute('srcdoc')).toContain('data:image/png;base64,iVBORw==')
    expect(frame.getAttribute('srcdoc')).toContain('img-src data:')
    expect(frame.getAttribute('srcdoc')).not.toContain('/api/v1/public/shares/')
    fireEvent.error(document.querySelector('img')!)
    await screen.findByText('图片加载失败')
  })

  it('does not execute raw Markdown HTML or fetch unlisted sources', async () => {
    server.use(http.get('/api/v1/public/shares/test-token', () => HttpResponse.json({ data: { ...content, body: '<script>window.bad=true</script>\n\n![外图](https://evil.test/track)\n\n安全正文', images: [] } })))
    open()
    await screen.findByText('安全正文')
    expect(document.querySelector('script')).toBeNull()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
  })

  it.each([false, true])('keeps the actual anonymous route outside workspace redirects with login=%s', async (loggedIn) => {
    if (loggedIn) {
      localStorage.setItem('anban_creator_token', 'stale-token')
      localStorage.setItem('anban_creator_user', JSON.stringify({ id: 'reader', name: '读者', is_admin: false }))
    }
    server.use(http.get('/api/v1/public/shares/test-token', () => HttpResponse.json({ data: content })))
    window.history.replaceState(null, '', '/share/test-token')
    const App = (await import('@/App')).default
    render(<App />)
    await screen.findByRole('heading', { name: content.title })
    expect(window.location.pathname).toBe('/share/test-token')
    expect(screen.queryByRole('navigation')).not.toBeInTheDocument()
  })
})
