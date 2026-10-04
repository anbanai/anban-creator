import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { render } from '@/test/test-utils'
import { ProjectIdentity } from './ProjectIdentity'

describe('ProjectIdentity', () => {
  it('shows the shared name and description without account identity', () => {
    render(<ProjectIdentity project={{ id: 'project', name: 'Morning Brief', platform: 'wechat', avatar_url: 'https://example.com/avatar.png', description: 'Daily editorial briefing' }} />)
    expect(screen.getByText('Morning Brief')).toBeInTheDocument()
    expect(screen.getByText('Daily editorial briefing')).toBeInTheDocument()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
    expect(document.querySelector('[data-slot="avatar"]')).not.toBeInTheDocument()
    expect(screen.queryByText('公众号项目')).not.toBeInTheDocument()
  })

  it('keeps compact identity limited to the project name', () => {
    render(<ProjectIdentity compact project={{ id: 'project', name: 'Garden Notes', description: 'Seasonal planting' }} />)
    expect(screen.getByText('Garden Notes')).toBeInTheDocument()
    expect(screen.queryByText('Seasonal planting')).not.toBeInTheDocument()
  })
})
