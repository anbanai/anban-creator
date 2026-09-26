import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { render } from '@/test/test-utils'
import { ProjectIdentity } from './ProjectIdentity'

describe('ProjectIdentity', () => {
  it.each([
    ['montage', '视频生成项目', 'text-purple-700'],
    ['hypit', '视频复刻项目', 'text-orange-700'],
  ])('preserves the %s identity in the project selector', (platform, label, color) => {
    render(<ProjectIdentity project={{ id: platform, name: '视频项目', platform }} />)
    expect(screen.getByText(label)).toHaveClass(color)
  })

  it('renders the project avatar, name, description, and localized type', () => {
    render(
      <ProjectIdentity
        project={{
          id: 'article-1',
          name: 'Morning Brief',
          platform: 'article',
          avatar_url: 'https://example.com/morning.png',
          description: 'Daily editorial briefing',
        }}
      />,
    )

    expect(screen.getByRole('img', { name: 'Morning Brief' })).toHaveAttribute(
      'src',
      'https://example.com/morning.png',
    )
    expect(screen.getByText('Morning Brief')).toBeInTheDocument()
    expect(screen.getByText('Daily editorial briefing')).toBeInTheDocument()
    expect(screen.getByText('公众号项目')).toBeInTheDocument()
  })

  it('uses the project initial without inventing a repeated type description', () => {
    const { rerender } = render(
      <ProjectIdentity
        project={{ id: 'seednote-1', name: '  Garden Notes  ', platform: 'seednote' }}
      />,
    )

    expect(screen.getByText('G')).toHaveClass('bg-[#FF2442]/10', 'text-[#FF2442]')
    expect(
      screen.getByText('种草笔记项目', { selector: '[data-slot="badge"]' }),
    ).toBeInTheDocument()
    expect(document.querySelector('[data-slot="project-identity-description"]')).not.toBeInTheDocument()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()

    rerender(
      <ProjectIdentity
        project={{ id: 'seednote-1', name: '  Garden Notes  ', platform: 'seednote' }}
        compact
      />,
    )
    expect(
      document.querySelector('[data-slot="project-identity-description"]'),
    ).not.toBeInTheDocument()
    expect(
      screen.getByText('种草笔记', { selector: '[data-slot="badge"]' }),
    ).toBeInTheDocument()
    expect(screen.queryByText('种草笔记项目')).not.toBeInTheDocument()
  })

  it('drops the project suffix in compact contexts so video brands stay readable', () => {
    const { rerender } = render(
      <ProjectIdentity project={{ id: 'hypit-1', name: 'Hypit', platform: 'hypit' }} />,
    )
    expect(screen.getByText('视频复刻项目')).toHaveClass('text-orange-700')

    rerender(
      <ProjectIdentity project={{ id: 'hypit-1', name: 'Hypit', platform: 'hypit' }} compact />,
    )
    const compactBadge = screen.getByText('视频复刻', { selector: '[data-slot="badge"]' })
    expect(compactBadge).toHaveClass('text-orange-700')
    expect(compactBadge).toHaveClass('shrink-0')
    expect(screen.getByText('Hypit')).toBeInTheDocument()
  })

  it('shows the initial when the project avatar fails to load', () => {
    render(
      <ProjectIdentity
        project={{
          id: 'article-1',
          name: 'Morning Brief',
          avatar_url: 'https://example.com/broken.png',
        }}
      />,
    )

    fireEvent.error(screen.getByRole('img', { name: 'Morning Brief' }))
    expect(screen.getByText('M')).toBeInTheDocument()
  })
})
