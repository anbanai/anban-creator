import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { render } from '@/test/test-utils'
import {
  ProjectContextControl,
  type ProjectContextProject,
} from './ProjectContextControl'

const projects: ProjectContextProject[] = [
  {
    id: 'article-1',
    name: 'Morning Brief',
    platform: 'wechat',
    agent_ids: ['wechat-article'],
    avatar_url: 'https://example.com/morning.png',
    description: 'Daily editorial briefing',
  },
  {
    id: 'seednote-1',
    name: 'Garden Notes',
    platform: 'seednote',
    agent_ids: ['seednote'],
    description: 'Seasonal planting journal',
  },
]

describe('ProjectContextControl', () => {
  it('shows capability icons with keyword tags together in one row and uses persona wording', async () => {
    render(
      <ProjectContextControl
        mode="select"
        projects={[{ ...projects[0], keywords: '茶知识, 茶文化' }]}
        value="article-1"
        onValueChange={vi.fn()}
      />,
    )

    const trigger = screen.getByRole('combobox', { name: '人设上下文' })
    fireEvent.click(trigger)
    const option = await screen.findByRole('option', { name: /Morning Brief/ })
    const detailsRow = option.querySelector('[data-slot="project-identity-details"]')

    expect(screen.getByPlaceholderText('搜索人设...')).toBeInTheDocument()
    expect(detailsRow?.querySelector('[data-agent-id="wechat-article"]')).toBeInTheDocument()
    expect(detailsRow).toHaveTextContent('茶知识')
    expect(detailsRow).toHaveTextContent('茶文化')
    expect(detailsRow?.querySelector('[data-slot="agent-icon"]')).not.toHaveTextContent('公众号文章')
    expect(detailsRow?.querySelector('[data-slot="project-identity-keywords"]')).toBeInTheDocument()
    expect(option).not.toHaveTextContent('公众号文章')
  })

  it('renders a controlled, searchable project combobox with grouped active options', async () => {
    const onValueChange = vi.fn()
    render(
      <ProjectContextControl
        mode="select"
        projects={projects}
        value="article-1"
        onValueChange={onValueChange}
      />,
    )
    const trigger = screen.getByRole('combobox', { name: '人设上下文' })
    expect(trigger).toHaveTextContent('Morning Brief')
    expect(trigger).toHaveTextContent('Daily editorial briefing')
    expect(trigger).not.toHaveTextContent('公众号人设')
    expect(trigger.querySelector('[data-slot="avatar"]')).toBeInTheDocument()
    expect(trigger).toHaveClass('border-border')

    fireEvent.click(trigger)
    const popup = screen.getByPlaceholderText('搜索人设...').closest('[data-slot="combobox-content"]')
    expect(popup).toHaveClass('w-[min(36rem,calc(100vw-2rem))]')
    expect(popup?.querySelector('[data-slot="combobox-list"]')).toHaveClass('sm:grid-cols-2')
    expect(screen.getAllByRole('img', { name: '公众号文章' }).length).toBeGreaterThan(0)
    expect(screen.getAllByRole('img', { name: '种草笔记' }).length).toBeGreaterThan(0)
    const selectedOption = screen.getByRole('option', { name: /Morning Brief.*Daily editorial briefing/ })
    expect(selectedOption).toHaveTextContent('Daily editorial briefing')
    expect(selectedOption.querySelector('[data-agent-id="wechat-article"]')).toBeInTheDocument()
    expect(selectedOption).not.toHaveTextContent('公众号文章')
    expect(selectedOption).toHaveAttribute('aria-selected', 'true')
    expect(selectedOption).toHaveClass('min-h-11')

    const search = screen.getByPlaceholderText('搜索人设...')
    fireEvent.change(search, { target: { value: 'Seasonal planting' } })
    expect(screen.queryByRole('option', { name: /Morning Brief/ })).not.toBeInTheDocument()
    const garden = screen.getByRole('option', { name: /Garden Notes/ })
    fireEvent.click(garden)

    expect(onValueChange).toHaveBeenCalledWith('seednote-1', projects[1])
  })

  it('supports an explicit no-project option', async () => {
    const onValueChange = vi.fn()
    render(
      <ProjectContextControl
        mode="select"
        projects={projects}
        value="article-1"
        onValueChange={onValueChange}
        allowNoProject
      />,
    )

    fireEvent.click(screen.getByRole('combobox', { name: '人设上下文' }))
    fireEvent.click(await screen.findByRole('option', { name: '不使用人设' }))

    expect(onValueChange).toHaveBeenCalledWith(null, undefined)
  })

  it('customizes the select no-project label without changing readonly semantics', async () => {
    const { rerender } = render(
      <ProjectContextControl
        mode="select"
        projects={projects}
        value={null}
        onValueChange={vi.fn()}
        allowNoProject
        noProjectLabel="全部人设"
      />,
    )

    const trigger = screen.getByRole('combobox', { name: '人设上下文' })
    expect(trigger).toHaveTextContent('全部人设')
    fireEvent.click(trigger)
    expect(await screen.findByRole('option', { name: '全部人设' })).toBeInTheDocument()

    rerender(
      <ProjectContextControl
        mode="readonly"
        project={null}
        noProjectLabel="独立作品"
      />,
    )
    expect(screen.getByText('独立作品')).toBeInTheDocument()
    expect(screen.queryByText('全部人设')).not.toBeInTheDocument()
  })

  it('shows the no-project value and links to project creation', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    render(
      <ProjectContextControl
        mode="select"
        projects={projects}
        value={null}
        onValueChange={vi.fn()}
        allowNoProject
        createProjectHref="/projects/new"
      />,
    )
    const trigger = screen.getByRole('combobox', { name: '人设上下文' })
    expect(trigger).toHaveTextContent('不使用人设')

    fireEvent.click(trigger)
    const link = await screen.findByRole('link', { name: '新建人设' })
    expect(link).toHaveAttribute('href', '/projects/new')
    await waitFor(() => {
      expect(consoleError).not.toHaveBeenCalledWith(
        expect.stringContaining('expected a native <button>'),
      )
    })
    consoleError.mockRestore()
  })

  it('renders readonly project and no-project labels without interactive controls', () => {
    const { rerender } = render(
      <ProjectContextControl mode="readonly" project={projects[0]} />,
    )
    expect(screen.getByText('Morning Brief')).toBeInTheDocument()
    expect(screen.getByText('Daily editorial briefing')).toBeInTheDocument()
    expect(screen.queryByText('公众号人设')).not.toBeInTheDocument()
    expect(document.querySelector('[data-slot="avatar"]')).toBeInTheDocument()
    expect(screen.queryByRole('img', { name: 'Morning Brief' })).not.toBeInTheDocument()
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument()

    rerender(
      <ProjectContextControl
        mode="readonly"
        project={null}
        noProjectLabel="Independent task"
      />,
    )
    expect(screen.getByText('Independent task')).toBeInTheDocument()
  })

  it('renders a compact readonly project without its description', () => {
    render(
      <ProjectContextControl mode="readonly" project={projects[0]} compact />,
    )

    expect(screen.getByText('Morning Brief')).toBeInTheDocument()
    expect(screen.getByRole('img', { name: '公众号文章' })).toBeInTheDocument()
    expect(document.querySelector('[data-slot="avatar"]')).toBeInTheDocument()
    expect(screen.queryByText('公众号人设')).not.toBeInTheDocument()
    expect(screen.queryByText('Daily editorial briefing')).not.toBeInTheDocument()
    expect(document.querySelector('[data-slot="project-context-control"]')).toHaveAttribute(
      'data-compact',
      'true',
    )
  })

  it('compacts only the trigger while keeping popup options complete', async () => {
    render(
      <ProjectContextControl
        mode="select"
        projects={projects}
        value="article-1"
        onValueChange={vi.fn()}
        compact
        ariaLabel="Choose project context"
      />,
    )

    const trigger = screen.getByRole('combobox', { name: 'Choose project context' })
    expect(trigger).toHaveTextContent('Morning Brief')
    expect(trigger.querySelector('[data-agent-id="wechat-article"]')).toBeInTheDocument()
    expect(trigger.querySelector('[data-slot="avatar"]')).toBeInTheDocument()
    expect(trigger.querySelector('[data-slot="project-capabilities"]')).toBeInTheDocument()
    expect(trigger.querySelector('[data-slot="project-capabilities"]')).not.toHaveTextContent('公众号文章')
    expect(trigger).toHaveClass('h-auto')
    expect(trigger).not.toHaveClass('border-border')
    expect(trigger).not.toHaveTextContent('Daily editorial briefing')
    expect(document.querySelector('[data-slot="project-context-control"]')).toHaveAttribute(
      'data-compact',
      'true',
    )

    fireEvent.click(trigger)
    const option = await screen.findByRole('option', { name: /Morning Brief.*Daily editorial briefing/ })
    expect(option).toHaveTextContent('Daily editorial briefing')
    expect(option.querySelector('[data-agent-id="wechat-article"]')).toBeInTheDocument()
    expect(option).not.toHaveTextContent('公众号文章')
  })

  it('finds a project by its specific content capability', async () => {
    render(
      <ProjectContextControl
        mode="select"
        projects={[{ id: 'picture-project', name: '图片内容号', platform: 'wechat', agent_ids: ['wechat-picture'] }]}
        value={null}
        onValueChange={vi.fn()}
      />,
    )

    fireEvent.click(screen.getByRole('combobox', { name: '人设上下文' }))
    const search = screen.getByPlaceholderText('搜索人设...')
    fireEvent.change(search, { target: { value: '公众号贴图' } })

    expect(await screen.findByRole('option', { name: /图片内容号.*公众号贴图/ })).toBeInTheDocument()
  })

  it('announces every selected content capability on the trigger', () => {
    render(
      <ProjectContextControl
        mode="select"
        projects={[{
          id: 'matrix',
          name: '内容矩阵',
          agent_ids: ['seednote', 'montage', 'hypit', 'wechat-article'],
        }]}
        value="matrix"
        onValueChange={vi.fn()}
        compact
        ariaLabel="筛选人设"
      />,
    )

    const trigger = screen.getByRole('combobox')
    expect(trigger).toHaveAccessibleDescription('内容矩阵，支持的内容类型：种草笔记、视频生成、视频复刻、公众号文章')
  })

  it('selects legacy projects without showing their old platform identity', async () => {
    render(
      <ProjectContextControl
        mode="select"
        projects={[
          { id: 'hypit-1', name: 'Hypit', platform: 'hypit', agent_ids: ['hypit'] },
          { id: 'montage-1', name: 'Montage', platform: 'montage', agent_ids: ['montage'] },
        ]}
        value="hypit-1"
        onValueChange={vi.fn()}
      />,
    )

    const trigger = screen.getByRole('combobox', { name: '人设上下文' })
    expect(trigger).not.toHaveTextContent('视频复刻人设')
    expect(trigger.querySelector('[data-project-mark="true"] [data-platform-icon]')).not.toBeInTheDocument()
    expect(trigger.querySelector('[data-agent-id="hypit"]')).toBeInTheDocument()

    fireEvent.click(trigger)
    expect(screen.getAllByRole('img', { name: '视频复刻' }).length).toBeGreaterThan(0)
    expect(screen.getAllByRole('img', { name: '视频生成' }).length).toBeGreaterThan(0)
    const replication = await screen.findByRole('option', { name: /Hypit/ })
    expect(replication).toHaveTextContent('Hypit')
    const generation = screen.getByRole('option', { name: /Montage/ })
    expect(generation).toHaveTextContent('Montage')
  })

  it('renders nothing in hidden mode', () => {
    render(<ProjectContextControl mode="hidden" />)
    expect(document.querySelector('[data-slot="project-context-control"]')).not.toBeInTheDocument()
  })

  it('disables the trigger and reports loading state', () => {
    render(
      <ProjectContextControl
        mode="select"
        projects={[]}
        value={null}
        onValueChange={vi.fn()}
        loading
      />,
    )
    const trigger = screen.getByRole('combobox', { name: '人设上下文' })
    expect(trigger).toBeDisabled()
    expect(trigger).toHaveTextContent('加载人设...')
    expect(document.querySelector('[data-slot="project-context-control"]')).toHaveAttribute('aria-busy', 'true')
    expect(screen.getByText('正在加载人设')).toBeInTheDocument()
  })

  it('keeps object selection stable while projects rerender with the popup open', async () => {
    const { rerender } = render(
      <ProjectContextControl
        mode="select"
        projects={projects}
        value="article-1"
        onValueChange={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByRole('combobox', { name: '人设上下文' }))
    expect(await screen.findByRole('option', { name: /Morning Brief/ })).toHaveAttribute('aria-selected', 'true')

    rerender(
      <ProjectContextControl
        mode="select"
        projects={projects.map((project) => ({ ...project }))}
        value="article-1"
        onValueChange={vi.fn()}
      />,
    )

    expect(screen.getByPlaceholderText('搜索人设...')).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /Morning Brief/ })).toHaveAttribute('aria-selected', 'true')
  })

  it('shows the configured placeholder and an empty result', async () => {
    render(
      <ProjectContextControl
        mode="select"
        projects={[]}
        value={null}
        onValueChange={vi.fn()}
        placeholder="Choose context"
      />,
    )
    const trigger = screen.getByRole('combobox', { name: '人设上下文' })
    expect(trigger).toHaveTextContent('Choose context')

    fireEvent.click(trigger)
    expect(await screen.findByText('没有可用人设')).toBeInTheDocument()
  })

  it('honors the disabled select state', () => {
    render(
      <ProjectContextControl
        mode="select"
        projects={projects}
        value="article-1"
        onValueChange={vi.fn()}
        disabled
      />,
    )
    expect(screen.getByRole('combobox', { name: '人设上下文' })).toBeDisabled()
  })
})
