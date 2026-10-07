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
    const generation = container.querySelector('[data-platform-icon="montage"]')
    expect(generation?.getAttribute('stroke-width')).toBe('1.75')
    const generationFrame = generation?.querySelector('rect')
    expect([generationFrame?.getAttribute('width'), generationFrame?.getAttribute('height'), generationFrame?.getAttribute('rx')]).toEqual(['18', '18', '5'])

    rerender(<>{renderPlatformIcon('hypit')}</>)
    const replication = container.querySelector('[data-platform-icon="hypit"]')
    expect(replication?.getAttribute('stroke-width')).toBe('1.75')
    const replicationFrames = replication!.querySelectorAll('rect')
    expect(replicationFrames).toHaveLength(2)
    for (const frame of replicationFrames) {
      expect(frame.getAttribute('width')).toBe('15')
      expect(frame.getAttribute('height')).toBe('15')
      expect(frame.getAttribute('rx')).toBe('4.25')
    }
  })

  it('keeps the play mark and light sweep inside the generation symbol', () => {
    const { container, rerender } = render(<>{renderPlatformIcon('montage')}</>)
    const symbol = container.querySelector('[data-platform-symbol="generate"]')
    const play = symbol!.querySelector('path')
    expect(play?.getAttribute('d')).toBe('M10.2 8.7v6.6a.55.55 0 0 0 .84.47l5.3-3.3a.55.55 0 0 0 0-.94l-5.3-3.3a.55.55 0 0 0-.84.47Z')
    expect(play).toHaveAttribute('fill', 'currentColor')
    const sweep = symbol!.querySelector('rect[fill="currentColor"]')
    expect([sweep?.getAttribute('width'), sweep?.getAttribute('height'), sweep?.getAttribute('x')]).toEqual(['3.5', '14', '6'])

    rerender(<>{renderPlatformIcon('hypit')}</>)
    const replicationSymbol = container.querySelector('[data-platform-symbol="replicate"]')
    expect(replicationSymbol!.querySelector('path')?.getAttribute('d')).toBe(
      'M9.2 11.2v4.6a.5.5 0 0 0 .76.43l3.9-2.3a.5.5 0 0 0 0-.86l-3.9-2.3a.5.5 0 0 0-.76.43Z',
    )
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

  it('gives every content agent its own glyph', () => {
    const types = ['wechat-article', 'wechat-picture', 'seednote', 'viral_analysis', 'moments', 'ecommerce']
    const seen = new Map<string, string>()
    const { container, rerender } = render(<>{renderPlatformIcon(types[0])}</>)
    for (const type of types) {
      rerender(<>{renderPlatformIcon(type)}</>)
      const shape = container.querySelector('svg')?.innerHTML ?? ''
      expect(shape, `${type} must render its own glyph`).toBeTruthy()
      expect(seen.has(shape), `${type} duplicates the ${seen.get(shape)} glyph`).toBe(false)
      seen.set(shape, type)
    }
  })

  it('does not render an unrelated icon for an unknown platform', () => {
    expect(renderPlatformIcon('unknown')).toBeNull()
  })
})
