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
    platform: 'article',
    avatar_url: 'https://example.com/morning.png',
    description: 'Daily editorial briefing',
  },
  {
    id: 'seednote-1',
    name: 'Garden Notes',
    platform: 'seednote',
    description: 'Seasonal planting journal',
  },
]

describe('ProjectContextControl', () => {
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
    const trigger = screen.getByRole('combobox', { name: '项目上下文' })
    expect(trigger).toHaveTextContent('Morning Brief')
    expect(trigger).toHaveTextContent('Daily editorial briefing')
    expect(trigger).not.toHaveTextContent('公众号项目')
    expect(trigger.querySelector('img')).toHaveAttribute('src', 'https://example.com/morning.png')
    expect(trigger).toHaveClass('border-border')

    fireEvent.click(trigger)
    const popup = screen.getByPlaceholderText('搜索项目...').closest('[data-slot="combobox-content"]')
    expect(popup).toHaveClass('w-[min(36rem,calc(100vw-2rem))]')
    expect(popup?.querySelector('[data-slot="combobox-list"]')).toHaveClass('sm:grid-cols-2')
    const articleGroup = (await screen.findByText('公众号')).closest('[data-platform]')
    expect(articleGroup).toHaveAttribute('data-platform', 'article')
    expect(articleGroup?.closest('[data-slot="combobox-label"]')).toHaveClass('py-0.5')
    expect(articleGroup?.closest('[data-slot="combobox-label"]')).not.toHaveTextContent('1 个')
    expect(screen.getByText('种草笔记')).toBeInTheDocument()
    const selectedOption = screen.getByRole('option', { name: /Morning Brief/ })
    expect(selectedOption).toHaveTextContent('Daily editorial briefing')
    expect(selectedOption).not.toHaveTextContent('公众号项目')
    expect(selectedOption).toHaveAttribute('aria-selected', 'true')
    expect(selectedOption).toHaveClass('min-h-10')

    const search = screen.getByPlaceholderText('搜索项目...')
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

    fireEvent.click(screen.getByRole('combobox', { name: '项目上下文' }))
    fireEvent.click(await screen.findByRole('option', { name: '不使用项目' }))

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
        noProjectLabel="全部项目"
      />,
    )

    const trigger = screen.getByRole('combobox', { name: '项目上下文' })
    expect(trigger).toHaveTextContent('全部项目')
    fireEvent.click(trigger)
    expect(await screen.findByRole('option', { name: '全部项目' })).toBeInTheDocument()

    rerender(
      <ProjectContextControl
        mode="readonly"
        project={null}
        noProjectLabel="独立任务"
      />,
    )
    expect(screen.getByText('独立任务')).toBeInTheDocument()
    expect(screen.queryByText('全部项目')).not.toBeInTheDocument()
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
    const trigger = screen.getByRole('combobox', { name: '项目上下文' })
    expect(trigger).toHaveTextContent('不使用项目')

    fireEvent.click(trigger)
    const link = await screen.findByRole('link', { name: '新建项目' })
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
    expect(screen.queryByText('公众号项目')).not.toBeInTheDocument()
    expect(screen.getByRole('img', { name: 'Morning Brief' })).toHaveAttribute(
      'src',
      'https://example.com/morning.png',
    )
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
    expect(screen.queryByText('公众号项目')).not.toBeInTheDocument()
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
    expect(trigger).not.toHaveTextContent('公众号项目')
    expect(trigger).not.toHaveTextContent('Daily editorial briefing')
    expect(document.querySelector('[data-slot="project-context-control"]')).toHaveAttribute(
      'data-compact',
      'true',
    )

    fireEvent.click(trigger)
    const option = await screen.findByRole('option', { name: /Morning Brief/ })
    expect(option).toHaveTextContent('Daily editorial briefing')
    expect(option).not.toHaveTextContent('公众号项目')
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
    const trigger = screen.getByRole('combobox', { name: '项目上下文' })
    expect(trigger).toBeDisabled()
    expect(trigger).toHaveTextContent('加载项目...')
    expect(document.querySelector('[data-slot="project-context-control"]')).toHaveAttribute('aria-busy', 'true')
    expect(screen.getByText('正在加载项目')).toBeInTheDocument()
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
    fireEvent.click(screen.getByRole('combobox', { name: '项目上下文' }))
    expect(await screen.findByRole('option', { name: /Morning Brief/ })).toHaveAttribute('aria-selected', 'true')

    rerender(
      <ProjectContextControl
        mode="select"
        projects={projects.map((project) => ({ ...project }))}
        value="article-1"
        onValueChange={vi.fn()}
      />,
    )

    expect(screen.getByPlaceholderText('搜索项目...')).toBeInTheDocument()
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
    const trigger = screen.getByRole('combobox', { name: '项目上下文' })
    expect(trigger).toHaveTextContent('Choose context')

    fireEvent.click(trigger)
    expect(await screen.findByText('没有可用项目')).toBeInTheDocument()
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
    expect(screen.getByRole('combobox', { name: '项目上下文' })).toBeDisabled()
  })
})
