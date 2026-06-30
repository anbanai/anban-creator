import { render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { SignedImage } from './SignedImage'
import http from '@/lib/http-client'

vi.mock('@/lib/http-client', () => ({
  default: {
    get: vi.fn(),
  },
}))

describe('SignedImage', () => {
  it('waits for internal blob URLs before rendering the image element', async () => {
    const createObjectURL = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:test-ref')
    const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    vi.mocked(http.get).mockResolvedValue({ data: new Blob(['x'], { type: 'image/png' }) })

    render(<SignedImage src="/api/v1/files/ref.png" alt="参考图" />)

    expect(screen.queryByRole('img', { name: '参考图' })).not.toBeInTheDocument()

    const image = await screen.findByRole('img', { name: '参考图' })
    await waitFor(() => {
      expect(image).toHaveAttribute('src', 'blob:test-ref')
    })
    expect(http.get).toHaveBeenCalledWith('/files/ref.png', { responseType: 'blob' })
    createObjectURL.mockRestore()
    revokeObjectURL.mockRestore()
  })
})
