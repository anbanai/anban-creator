import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { Badge } from '@/components/ui/badge'
import { platformBadgeClassName, platformBgColor, platformBorderColor, platformHoverBorderColor, platformIconColor, renderPlatformIcon } from './PlatformIcon'

describe('video platform identity', () => {
  it('renders video-first generation and replication icons', () => {
    const { container, rerender } = render(<>{renderPlatformIcon('montage')}</>)
    const generation = container.querySelector('[data-platform-icon="montage"]')
    expect(generation?.tagName).toBe('svg')
    expect(generation).toHaveClass('text-[#9333EA]')
    expect(generation?.querySelector('[data-platform-symbol="sparkles"]')).toBeInTheDocument()

    rerender(<>{renderPlatformIcon('hypit')}</>)
    const replication = container.querySelector('[data-platform-icon="hypit"]')
    expect(replication?.tagName).toBe('svg')
    expect(replication).toHaveClass('text-[#F97316]')
    expect(replication?.querySelector('[data-platform-symbol="copy"]')).toBeInTheDocument()
  })

  it('keeps composite icons as direct SVG children in compact badges', () => {
    const { getByTestId, rerender } = render(<Badge data-testid="video-badge">{renderPlatformIcon('montage')}</Badge>)
    const generationBadge = getByTestId('video-badge')
    expect(generationBadge.firstElementChild?.tagName).toBe('svg')
    expect(generationBadge.querySelector('[data-platform-symbol="sparkles"]')).toBeInTheDocument()

    rerender(<Badge data-testid="video-badge">{renderPlatformIcon('hypit')}</Badge>)
    const replicationBadge = getByTestId('video-badge')
    expect(replicationBadge.firstElementChild?.tagName).toBe('svg')
    expect(replicationBadge.querySelector('[data-platform-symbol="copy"]')).toBeInTheDocument()
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
