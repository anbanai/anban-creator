import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { render } from '@/test/test-utils'
import { ProjectIdentity } from './ProjectIdentity'

describe('ProjectIdentity', () => {
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

  it('uses the project initial and deterministic localized fallback without optional metadata', () => {
    const { rerender } = render(
      <ProjectIdentity
        project={{ id: 'seednote-1', name: '  Garden Notes  ', platform: 'seednote' }}
      />,
    )

    expect(screen.getByText('G')).toBeInTheDocument()
    expect(
      screen.getByText('种草笔记项目', { selector: '[data-slot="badge"]' }),
    ).toBeInTheDocument()
    expect(
      screen.getByText('种草笔记项目', {
        selector: '[data-slot="project-identity-description"]',
      }),
    ).toBeInTheDocument()
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
      screen.getByText('种草笔记项目', { selector: '[data-slot="badge"]' }),
    ).toBeInTheDocument()
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
