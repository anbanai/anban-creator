import { fireEvent, render } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { Badge } from '@/components/ui/badge'
import { platformBadgeClassName, platformBgColor, platformBorderColor, platformHoverBorderColor, platformIconColor, renderPlatformIcon } from './PlatformIcon'

const motionState = vi.hoisted(() => {
  const controls = {
    start: vi.fn(),
    stop: vi.fn(),
    set: vi.fn(),
    mount: vi.fn(),
    subscribe: () => () => {},
  }
  return { controls, reduced: false }
})

vi.mock('motion/react', async (importOriginal) => {
  const actual = await importOriginal<typeof import('motion/react')>()
  return {
    ...actual,
    useAnimation: () => motionState.controls,
    useReducedMotion: () => motionState.reduced,
  }
})

describe('video platform identity', () => {
  beforeEach(() => {
    motionState.controls.start.mockClear()
    motionState.reduced = false
  })

  it('renders video-first generation and replication icons', () => {
    const { container, rerender } = render(<>{renderPlatformIcon('montage')}</>)
    const generation = container.querySelector('[data-platform-icon="montage"]')
    expect(generation?.tagName).toBe('svg')
    expect(generation).toHaveClass('text-[#9333EA]')
    expect(generation?.querySelector('[data-platform-symbol="generate"]')).toBeInTheDocument()

    rerender(<>{renderPlatformIcon('hypit')}</>)
    const replication = container.querySelector('[data-platform-icon="hypit"]')
    expect(replication?.tagName).toBe('svg')
    expect(replication).toHaveClass('text-[#F97316]')
    expect(replication?.querySelector('[data-platform-symbol="replicate"]')).toBeInTheDocument()
  })

  it('keeps composite icons as direct SVG children in compact badges', () => {
    const { getByTestId, rerender } = render(<Badge data-testid="video-badge">{renderPlatformIcon('montage')}</Badge>)
    const generationBadge = getByTestId('video-badge')
    expect(generationBadge.firstElementChild?.tagName).toBe('svg')
    expect(generationBadge.querySelector('[data-platform-symbol="generate"]')).toBeInTheDocument()

    rerender(<Badge data-testid="video-badge">{renderPlatformIcon('hypit')}</Badge>)
    const replicationBadge = getByTestId('video-badge')
    expect(replicationBadge.firstElementChild?.tagName).toBe('svg')
    expect(replicationBadge.querySelector('[data-platform-symbol="replicate"]')).toBeInTheDocument()
  })

  it('draws both video marks from the same frame geometry', () => {
    const { container, rerender } = render(<>{renderPlatformIcon('montage')}</>)
    const generationFrame = container.querySelector('[data-platform-icon="montage"] rect')
    expect(generationFrame?.getAttribute('width')).toBe('18')
    expect(generationFrame?.getAttribute('rx')).toBe('5')

    rerender(<>{renderPlatformIcon('hypit')}</>)
    const replicationFrames = container.querySelectorAll('[data-platform-icon="hypit"] rect')
    expect(replicationFrames).toHaveLength(2)
    for (const frame of replicationFrames) {
      expect(frame.getAttribute('width')).toBe('15')
      expect(frame.getAttribute('rx')).toBe('4.25')
    }
  })

  it.each(['montage', 'hypit'] as const)('animates %s on hover and settles on leave', (platform) => {
    const { container } = render(<>{renderPlatformIcon(platform)}</>)
    const icon = container.querySelector(`[data-platform-icon="${platform}"]`)

    fireEvent.mouseEnter(icon!)
    expect(motionState.controls.start).toHaveBeenCalledWith('animate')

    motionState.controls.start.mockClear()
    fireEvent.mouseLeave(icon!)
    expect(motionState.controls.start).toHaveBeenCalledWith('normal')
  })

  it('stays static when the user prefers reduced motion', () => {
    motionState.reduced = true
    const { container } = render(<>{renderPlatformIcon('montage')}</>)
    const icon = container.querySelector('[data-platform-icon="montage"]')

    fireEvent.mouseEnter(icon!)
    fireEvent.mouseLeave(icon!)

    expect(motionState.controls.start).not.toHaveBeenCalled()
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
