import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { platformBadgeClassName, platformBgColor, platformBorderColor, platformHoverBorderColor, platformIconColor, renderPlatformIcon } from './PlatformIcon'

describe('video platform identity', () => {
  it('renders distinct generation and replication icons', () => {
    const { container, rerender } = render(<>{renderPlatformIcon('montage')}</>)
    expect(container.querySelector('svg')).toHaveClass('lucide-clapperboard', 'text-[#9333EA]')
    rerender(<>{renderPlatformIcon('hypit')}</>)
    expect(container.querySelector('svg')).toHaveClass('lucide-repeat-2', 'text-[#F97316]')
  })

  it('keeps platform accents distinct across cards, badges and selectors', () => {
    for (const mapping of [platformBadgeClassName, platformBgColor, platformBorderColor, platformHoverBorderColor, platformIconColor]) {
      expect(mapping.montage).toBeTruthy()
      expect(mapping.hypit).toBeTruthy()
      expect(mapping.montage).not.toBe(mapping.hypit)
    }
    expect(platformBadgeClassName.montage).toContain('text-purple-700')
    expect(platformBadgeClassName.hypit).toContain('text-orange-700')
  })

  it('does not render an unrelated icon for an unknown platform', () => {
    expect(renderPlatformIcon('unknown')).toBeNull()
  })
})
