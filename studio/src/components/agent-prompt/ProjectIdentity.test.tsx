import { screen, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { render } from '@/test/test-utils'
import { ProjectIdentity } from './ProjectIdentity'

describe('ProjectIdentity', () => {
  it('shows a project avatar, platform label, tags, and description', async () => {
    render(<ProjectIdentity project={{ id: 'project', name: 'Morning Brief', platform: 'wechat', avatar_url: 'https://example.com/avatar.png', description: 'Daily editorial briefing', keywords: '科技, 资讯, 每日' }} />)
    expect(screen.getByText('Morning Brief')).toBeInTheDocument()
    expect(screen.getByText('Daily editorial briefing')).toBeInTheDocument()
    expect(document.querySelector('[data-slot="avatar"]')).toBeInTheDocument()
    expect(screen.getByLabelText(/Morning Brief，公众号，科技/)).toBeInTheDocument()
    await waitFor(() => expect(document.querySelector('[data-slot="avatar"] img')).toHaveAttribute('src', 'https://example.com/avatar.png'))
    expect(screen.getByText('公众号')).toBeInTheDocument()
    expect(screen.getByText('科技')).toBeInTheDocument()
    expect(screen.getByText('资讯')).toBeInTheDocument()
    expect(screen.getByText('+1')).toHaveAttribute('title', '其余标签：每日')
    expect(screen.queryByText('每日')).not.toBeInTheDocument()
  })

  it('keeps compact identity scannable with avatar and platform label', () => {
    render(<ProjectIdentity compact project={{ id: 'project', name: 'Garden Notes', platform: 'seednote', description: 'Seasonal planting' }} />)
    expect(screen.getByText('Garden Notes')).toBeInTheDocument()
    expect(screen.queryByText('Seasonal planting')).not.toBeInTheDocument()
    expect(screen.getByText('种草笔记')).toBeInTheDocument()
    expect(document.querySelector('[data-slot="avatar"]')).toBeInTheDocument()
  })

  it('uses a stable fallback avatar when no image is available', () => {
    render(<ProjectIdentity project={{ id: 'project', name: 'Garden Notes' }} />)
    expect(screen.getByLabelText('Garden Notes')).toBeInTheDocument()
    expect(screen.getByText('GN')).toBeInTheDocument()
  })

  it('uses project instructions as the summary when no description is available', () => {
    render(<ProjectIdentity project={{ id: 'project', name: 'Garden Notes', instructions: '面向园艺新手的季节性种植内容' }} />)
    expect(screen.getByText('面向园艺新手的季节性种植内容')).toBeInTheDocument()
  })

  it('hides unknown internal platform ids behind a user-facing fallback label', () => {
    render(<ProjectIdentity project={{ id: 'project', name: 'Plugin Project', platform: 'internal_plugin_v2' }} />)
    expect(screen.getByText('通用项目')).toBeInTheDocument()
    expect(screen.queryByText('internal_plugin_v2')).not.toBeInTheDocument()
  })
})
