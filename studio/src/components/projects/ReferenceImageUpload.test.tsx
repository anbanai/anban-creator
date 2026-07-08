import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { ReferenceImageUpload } from './ReferenceImageUpload'

vi.mock('@/lib/http-client', () => ({
  default: { get: vi.fn() },
}))

vi.mock('@/lib/direct-upload', () => ({
  uploadToOSS: vi.fn(),
}))

describe('ReferenceImageUpload', () => {
  it('exposes preview and remove actions for an existing reference image', async () => {
    const onChange = vi.fn()

    render(
      <ReferenceImageUpload
        value="https://cdn.example.com/reference.png"
        onChange={onChange}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '移除参考图' }))
    expect(onChange).toHaveBeenCalledWith('')

    fireEvent.click(screen.getByRole('button', { name: '查看参考图' }))
    expect(await screen.findByAltText('参考图放大')).toBeInTheDocument()
  })
})
