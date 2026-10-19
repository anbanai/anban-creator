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
    expect(document.querySelector('[data-slot="project-capabilities"]')).toBeInTheDocument()
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

  it('uses distinct icons for WeChat articles and picture posts', () => {
    render(<ProjectIdentity project={{ id: 'wechat-types', name: '公众号矩阵', agent_ids: ['wechat-article', 'wechat-picture'] }} />)

    expect(document.querySelector('[data-agent-id="wechat-article"] svg')).not.toHaveClass('lucide-images')
    expect(document.querySelector('[data-agent-id="wechat-picture"] svg')).toHaveClass('lucide-images')
    expect(screen.getByLabelText('关联 Agent：公众号文章、公众号贴图')).toBeInTheDocument()
  })

  it('keeps compact identity scannable with the Agent icon below the project name', () => {
    render(<ProjectIdentity compact project={{ id: 'project', name: 'Garden Notes', platform: 'seednote', agent_ids: ['seednote'], description: 'Seasonal planting' }} />)
    expect(screen.getByText('Garden Notes')).toBeInTheDocument()
    expect(screen.queryByText('Seasonal planting')).not.toBeInTheDocument()
    expect(document.querySelector('[data-agent-id="seednote"]')).toBeInTheDocument()
    const identityDetails = screen.getByText('Garden Notes').parentElement?.parentElement
    expect(identityDetails).toHaveClass('flex-col')
    expect(identityDetails?.querySelector('[data-slot="project-capabilities"]')).toBeInTheDocument()
    expect(document.querySelector('[data-slot="avatar"]')).toBeInTheDocument()
    expect(screen.queryByText('种草笔记')).not.toBeInTheDocument()
  })

  it('collapses capability labels after three items while exposing the complete list', () => {
    render(<ProjectIdentity showCapabilityLabels project={{ id: 'many', name: '内容矩阵', agent_ids: ['seednote', 'montage', 'hypit', 'wechat-article'] }} />)

    expect(screen.getByText('种草笔记')).toBeInTheDocument()
    expect(screen.getByText('视频生成')).toBeInTheDocument()
    expect(screen.getByText('视频复刻')).toBeInTheDocument()
    const overflow = screen.getByLabelText('支持的内容类型：种草笔记、视频生成、视频复刻、公众号文章')
    expect(overflow).not.toHaveAttribute('tabindex')
    expect(overflow).toHaveAttribute('title', '支持的内容类型：种草笔记、视频生成、视频复刻、公众号文章')
    expect(screen.queryByText('公众号文章')).not.toBeInTheDocument()
  })

  it('uses the platform default Agent until a project has durable Agent activity', () => {
    render(<ProjectIdentity compact project={{ id: 'fresh', name: '新种草创作项目', platform: 'seednote' }} />)

    expect(document.querySelector('[data-agent-id="seednote"]')).toBeInTheDocument()
    expect(screen.queryByText('尚未关联 Agent')).not.toBeInTheDocument()
  })

  it('respects an explicitly empty capability collection without deriving from the platform', () => {
    render(<ProjectIdentity project={{ id: 'empty', name: '清空了创作对象', platform: 'seednote', agent_ids: [] }} />)
    expect(document.querySelector('[data-agent-id="seednote"]')).not.toBeInTheDocument()
    expect(screen.getByText('尚未关联 Agent')).toBeInTheDocument()
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
