import { describe, expect, it } from 'vitest'

import { buildDesignerRequestSize } from './designer-size'

describe('buildDesignerRequestSize', () => {
  it('keeps auto size independent of resolution tier', () => {
    expect(buildDesignerRequestSize('auto', '4K')).toBe('auto')
  })

  it('keeps pixel sizes independent of resolution tier', () => {
    expect(buildDesignerRequestSize('1024x1024', '4K')).toBe('1024x1024')
  })

  it('appends non-default resolution tier to ratio sizes', () => {
    expect(buildDesignerRequestSize('3:4', '4K')).toBe('3:4:4K')
  })

  it('keeps 2K ratio sizes unsuffixed', () => {
    expect(buildDesignerRequestSize('3:4', '2K')).toBe('3:4')
  })
})
