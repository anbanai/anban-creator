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

  it('draws the video marks from unmodified lucide geometry', () => {
    const { container, rerender } = render(<>{renderPlatformIcon('montage')}</>)
    const generation = container.querySelector('[data-platform-icon="montage"]')
    expect(generation?.getAttribute('stroke-width')).toBe('2')
    expect(generation).toHaveStyle({ overflow: 'visible' })
    expect([...generation!.querySelectorAll('path')].map((path) => path.getAttribute('d'))).toEqual([
      'm12.296 3.464 3.02 3.956',
      'M20.2 6 3 11l-.9-2.4c-.3-1.1.3-2.2 1.3-2.5l13.5-4c1.1-.3 2.2.3 2.5 1.3z',
      'm6.18 5.276 3.1 3.899',
      'M3 11h18v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z',
    ])

    rerender(<>{renderPlatformIcon('hypit')}</>)
    const replication = container.querySelector('[data-platform-icon="hypit"]')
    expect(replication?.getAttribute('stroke-width')).toBe('2')
    expect(replication).toHaveStyle({ overflow: 'visible' })
    expect(replication?.querySelector('path')?.getAttribute('d')).toBe(
      'M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2',
    )
    const sheet = replication?.querySelector('rect')
    expect([sheet?.getAttribute('width'), sheet?.getAttribute('height'), sheet?.getAttribute('rx')]).toEqual(['14', '14', '2'])
  })

  it('animates the clapper lid together with its stripes', () => {
    const { container } = render(<>{renderPlatformIcon('montage')}</>)
    const symbol = container.querySelector('[data-platform-symbol="generate"]')
    const animated = symbol!.querySelector('g')
    expect(animated?.tagName).toBe('g')

    const lidGroupPaths = [...animated!.querySelectorAll('path')].map((path) => path.getAttribute('d'))
    expect(lidGroupPaths).toHaveLength(3)
    expect(lidGroupPaths).toContain('M20.2 6 3 11l-.9-2.4c-.3-1.1.3-2.2 1.3-2.5l13.5-4c1.1-.3 2.2.3 2.5 1.3z')
    expect(lidGroupPaths).toContain('m12.296 3.464 3.02 3.956')
    expect(lidGroupPaths).toContain('m6.18 5.276 3.1 3.899')
    expect([...symbol!.querySelectorAll(':scope > path')].map((path) => path.getAttribute('d'))).toEqual([
      'M3 11h18v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z',
    ])
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

  it('distinguishes the wechat article and picture task glyphs', () => {
    const { container, rerender } = render(<>{renderPlatformIcon('wechat-article')}</>)
    const article = container.querySelector('svg')
    expect(article).toBeInTheDocument()
    const articlePaths = [...article!.querySelectorAll('path')].map((path) => path.getAttribute('d'))

    rerender(<>{renderPlatformIcon('wechat-picture')}</>)
    const picture = container.querySelector('svg')
    expect(picture).toBeInTheDocument()
    const picturePaths = [...picture!.querySelectorAll('path')].map((path) => path.getAttribute('d'))

    expect(picturePaths).not.toEqual(articlePaths)
    expect(renderPlatformIcon('wechat-article')).not.toEqual(renderPlatformIcon('wechat-picture'))
  })

  it('does not render an unrelated icon for an unknown platform', () => {
    expect(renderPlatformIcon('unknown')).toBeNull()
  })
})
