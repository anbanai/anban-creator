import { screen, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { render } from '@/test/test-utils'
import { ProjectIdentity } from './ProjectIdentity'

describe('ProjectIdentity', () => {
  it('keeps the project avatar neutral and renders multiple Agent icons separately', () => {
    render(<ProjectIdentity project={{ id: 'project', name: '茶内容矩阵', platform: 'montage', agent_ids: ['seednote', 'montage', 'hypit'], description: '面向茶内容的多渠道创作' }} />)

    expect(document.querySelector('[data-project-mark="true"]')).toBeInTheDocument()
    expect(document.querySelector('[data-project-mark="true"] [data-platform-icon]')).not.toBeInTheDocument()
    expect(document.querySelectorAll('[data-slot="agent-icon"]')).toHaveLength(3)
    expect(screen.getByLabelText('关联 Agent：种草笔记、视频生成、视频复刻')).toBeInTheDocument()
    expect(screen.queryByText('视频生成')).not.toBeInTheDocument()
  })

  it('shows a project avatar, Agent icon, keywords, and description', async () => {
    render(<ProjectIdentity project={{ id: 'project', name: 'Morning Brief', platform: 'wechat', agent_ids: ['wechat-article'], avatar_url: 'https://example.com/avatar.png', description: 'Daily editorial briefing', keywords: '科技, 资讯, 每日, 内容' }} />)
    expect(screen.getByText('Morning Brief')).toBeInTheDocument()
    expect(screen.getByText('Daily editorial briefing')).toBeInTheDocument()
    expect(document.querySelector('[data-slot="avatar"]')).toBeInTheDocument()
    expect(screen.getByLabelText(/Morning Brief，公众号文章，科技/)).toBeInTheDocument()
    await waitFor(() => expect(document.querySelector('[data-slot="avatar"] img')).toHaveAttribute('src', 'https://example.com/avatar.png'))
    expect(document.querySelector('[data-agent-id="wechat-article"]')).toBeInTheDocument()
    expect(screen.getByText('科技')).toBeInTheDocument()
    expect(screen.getByText('资讯')).toBeInTheDocument()
    expect(screen.getByText('+1')).toHaveAttribute('title', '其余关键词：内容')
    expect(screen.getByText('每日')).toBeInTheDocument()
  })

  it('keeps compact identity scannable with avatar and Agent icon', () => {
    render(<ProjectIdentity compact project={{ id: 'project', name: 'Garden Notes', platform: 'seednote', agent_ids: ['seednote'], description: 'Seasonal planting' }} />)
    expect(screen.getByText('Garden Notes')).toBeInTheDocument()
    expect(screen.queryByText('Seasonal planting')).not.toBeInTheDocument()
    expect(document.querySelector('[data-agent-id="seednote"]')).toBeInTheDocument()
    expect(document.querySelector('[data-slot="avatar"]')).toBeInTheDocument()
  })

  it('uses the platform default Agent until a project has durable Agent activity', () => {
    render(<ProjectIdentity compact project={{ id: 'fresh', name: '新种草项目', platform: 'seednote' }} />)

    expect(document.querySelector('[data-agent-id="seednote"]')).toBeInTheDocument()
    expect(screen.queryByText('尚未关联 Agent')).not.toBeInTheDocument()
  })

  it('uses a neutral project mark when no image is available', () => {
    render(<ProjectIdentity project={{ id: 'project', name: 'Garden Notes' }} />)
    expect(screen.getByLabelText('Garden Notes')).toBeInTheDocument()
    expect(document.querySelector('[data-project-mark="true"]')).toBeInTheDocument()
  })

  it('uses project instructions as the summary when no description is available', () => {
    render(<ProjectIdentity project={{ id: 'project', name: 'Garden Notes', instructions: '面向园艺新手的季节性种植内容' }} />)
    expect(screen.getByText('面向园艺新手的季节性种植内容')).toBeInTheDocument()
  })

  it('does not expose unknown platform ids as project identity tags', () => {
    render(<ProjectIdentity project={{ id: 'project', name: 'Plugin Project', platform: 'internal_plugin_v2' }} />)
    expect(screen.getByText('尚未关联 Agent')).toBeInTheDocument()
    expect(screen.queryByText('internal_plugin_v2')).not.toBeInTheDocument()
  })
})
