import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import { mockProjectDetail, mockTasks } from '@/test/mocks/handlers'
import { normalizeProjectProfile } from '@/lib/project-profile'
import type { Task } from '@/types'
import { TaskCreationSidebar } from './TaskCreationSidebar'

const task: Task = {
  ...mockTasks.items[0],
  status: 'running',
  billing_price_credits: 500,
  billing_total_credits: 1100,
  project_snapshot: { channel: 'wechat-article', image_ratio: '3:4' },
  article_with_cover: true,
  article_with_content_images: true,
  input_attachments: [{ type: 'image', file_name: 'reference.png' }],
}

function renderSidebar(overrides: Partial<Parameters<typeof TaskCreationSidebar>[0]> = {}) {
  const props = {
    task,
    project: { ...mockProjectDetail.project, name: '当前茶小茶', image_ratio: '1:1' },
    profile: normalizeProjectProfile({ dimensions: {
      identity: { content: { positioning: '分享日常茶饮知识' } },
      audience: { content: { summary: '喜欢茶饮的年轻人' } },
      style: { content: { summary: '温柔清晰' } },
    } }),
    onOpenPersona: vi.fn(),
    onOpenTab: vi.fn(),
    ...overrides,
  }
  return { ...render(<TaskCreationSidebar {...props} />), props }
}

describe('TaskCreationSidebar', () => {
  it('shows restrained current persona facts and frozen creation settings while running', () => {
    renderSidebar()
    const panel = screen.getByRole('region', { name: '创作参考' })
    expect(within(panel).getByText('当前人设')).toBeInTheDocument()
    expect(within(panel).getByText('当前茶小茶')).toBeInTheDocument()
    expect(within(panel).getByText('分享日常茶饮知识')).toBeInTheDocument()
    expect(within(panel).getByText('喜欢茶饮的年轻人')).toBeInTheDocument()
    expect(panel).not.toHaveTextContent('温柔清晰')
    expect(panel).toHaveTextContent('公众号文章')
    expect(panel).toHaveTextContent('封面 + 正文配图')
    expect(panel).toHaveTextContent('3:4')
    expect(panel).not.toHaveTextContent('1:1')
    expect(panel).toHaveTextContent('参考素材 1 项')
    expect(panel).not.toHaveTextContent(/积分|手动创建|未设置|0 项|runtime/)
  })

  it('omits empty profile facts and does not use current settings for a sparse snapshot', () => {
    renderSidebar({
      profile: normalizeProjectProfile({ dimensions: { identity: { content: { nested: { raw: 'secret' } } } } }),
      project: { ...mockProjectDetail.project, description: '', instructions: '', positioning: '', image_ratio: '16:9' },
      task: { ...task, project_snapshot: {}, article_with_cover: undefined, article_with_content_images: undefined, input_attachments: [] },
    })
    const panel = screen.getByRole('region', { name: '创作参考' })
    expect(panel).not.toHaveTextContent(/定位|受众|风格|16:9|尚未|secret|0 项|参考素材/)
    expect(screen.queryByRole('button', { name: '打开参考素材' })).not.toBeInTheDocument()
  })

  it('identifies an unavailable persona from its frozen name without current profile claims', () => {
    renderSidebar({ project: undefined, task: { ...task, project_snapshot: { project_name: '创建时茶小茶' } } })
    const panel = screen.getByRole('region', { name: '创作参考' })
    expect(panel).toHaveTextContent('创建时人设')
    expect(panel).toHaveTextContent('创建时茶小茶')
    expect(panel).not.toHaveTextContent(/当前人设|分享日常茶饮知识|喜欢茶饮/)
    expect(screen.queryByRole('button', { name: '打开完整人设' })).not.toBeInTheDocument()
  })

  it('uses current ratio only for legacy tasks without a snapshot and prefers task settings', () => {
    const { rerender, props } = renderSidebar({ task: { ...task, project_snapshot: undefined } })
    expect(screen.getByText(/画幅 1:1/)).toBeInTheDocument()
    rerender(<TaskCreationSidebar {...props} task={{ ...task, image_ratio: '9:16' }} />)
    expect(screen.getByText(/画幅 9:16/)).toBeInTheDocument()
    expect(screen.queryByText(/画幅 3:4/)).not.toBeInTheDocument()
  })

  it('omits an empty persona section and empty creation metadata', () => {
    renderSidebar({
      project: undefined,
      profile: null,
      task: { ...task, type: 'profile-analysis', project_snapshot: {}, article_with_cover: undefined, article_with_content_images: undefined, input_attachments: [] },
    })
    expect(screen.queryByRole('region', { name: '创作参考' })).not.toBeInTheDocument()
  })

  it.each(['pending', 'running', 'awaiting_review', 'failed', 'cancelled'] as const)('hides billing for %s tasks', (status) => {
    renderSidebar({ task: { ...task, status } })
    expect(screen.getByRole('region', { name: '创作参考' })).not.toHaveTextContent('积分')
  })

  it.each([
    [1100, '1,100'],
    [0, '0'],
    [undefined, '500'],
  ])('shows only the final Server total %s for completed tasks', (total, label) => {
    renderSidebar({ task: { ...task, status: 'completed', billing_total_credits: total } })
    expect(screen.getByText(`总消耗 ${label} 积分`)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /积分|扣费/ })).not.toBeInTheDocument()
  })

  it('opens existing persona, configuration and materials details', () => {
    const { props } = renderSidebar()
    fireEvent.click(screen.getByRole('button', { name: '打开完整人设' }))
    fireEvent.click(screen.getByRole('button', { name: '打开创作配置' }))
    fireEvent.click(screen.getByRole('button', { name: '打开参考素材' }))
    expect(props.onOpenPersona).toHaveBeenCalledOnce()
    expect(vi.mocked(props.onOpenTab).mock.calls).toEqual([['configuration'], ['materials']])
  })
})
