import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { render } from '@/test/test-utils'
import { ProjectIdentity } from './ProjectIdentity'

describe('ProjectIdentity', () => {
  it.each([
    ['montage', 'text-[#9333EA]'],
    ['hypit', 'text-[#F97316]'],
  ])('uses the %s type mark as the avatar without a repeated type label', (platform, color) => {
    render(<ProjectIdentity project={{ id: platform, name: '视频项目', platform }} />)
    const icon = platform === 'montage'
      ? document.querySelector('[data-platform-icon="montage"]')
      : document.querySelector('[data-platform-icon="hypit"]')
    expect(icon).toBeInTheDocument()
    expect(icon).toHaveClass(color)
    expect(screen.queryByText(/视频生成项目|视频复刻项目/)).not.toBeInTheDocument()
  })

  it('renders the type avatar, project name, and description without a type badge', () => {
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

    expect(document.querySelector('.lucide-signature')).toBeInTheDocument()
    expect(screen.getByRole('img', { name: '公众号类型' })).toHaveAttribute('title', '公众号类型')
    expect(screen.queryByRole('img', { name: 'Morning Brief' })).not.toBeInTheDocument()
    expect(screen.getByText('Morning Brief')).toBeInTheDocument()
    expect(screen.getByText('Daily editorial briefing')).toBeInTheDocument()
    expect(screen.queryByText('公众号项目')).not.toBeInTheDocument()
  })

  it('uses the project initial when no platform mark or avatar is available', () => {
    const { rerender } = render(
      <ProjectIdentity
        project={{ id: 'unknown-1', name: '  Garden Notes  ' }}
      />,
    )

    expect(screen.getByText('G')).toBeInTheDocument()
    expect(document.querySelector('[data-slot="project-identity-description"]')).not.toBeInTheDocument()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()

    rerender(
      <ProjectIdentity
        project={{ id: 'unknown-1', name: '  Garden Notes  ' }}
        compact
      />,
    )
    expect(
      document.querySelector('[data-slot="project-identity-description"]'),
    ).not.toBeInTheDocument()
    expect(screen.queryByText('种草笔记')).not.toBeInTheDocument()
  })

  it('can use the platform mark as the avatar for compact project controls', () => {
    render(
      <ProjectIdentity
        project={{ id: 'hypit-1', name: 'Hypit', platform: 'hypit' }}
        avatarMode="platform"
        showType={false}
        compact
      />,
    )

    expect(screen.getByText('Hypit')).toBeInTheDocument()
    expect(screen.queryByText('视频复刻')).not.toBeInTheDocument()
    expect(document.querySelector('[data-slot="avatar-fallback"] svg')).toBeInTheDocument()
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
