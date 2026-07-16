import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { render } from '@/test/test-utils'
import {
  ProjectContextControl,
  type ProjectContextProject,
} from './ProjectContextControl'

const projects: ProjectContextProject[] = [
  { id: 'article-1', name: 'Morning Brief', platform: 'article' },
  { id: 'seednote-1', name: 'Garden Notes', platform: 'seednote' },
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
    expect(trigger).toHaveClass('border-border')

    fireEvent.click(trigger)
    expect(await screen.findByText('公众号')).toBeInTheDocument()
    expect(screen.getByText('种草笔记')).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Morning Brief' })).toHaveAttribute('aria-selected', 'true')

    const search = screen.getByPlaceholderText('搜索项目...')
    fireEvent.change(search, { target: { value: 'Garden' } })
    expect(screen.queryByRole('option', { name: 'Morning Brief' })).not.toBeInTheDocument()
    const garden = screen.getByRole('option', { name: 'Garden Notes' })
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

  it('shows the no-project value and links to project creation', async () => {
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
  })

  it('renders readonly project and no-project labels without interactive controls', () => {
    const { rerender } = render(
      <ProjectContextControl mode="readonly" project={projects[0]} />,
    )
    expect(screen.getByText('Morning Brief')).toBeInTheDocument()
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
    expect(screen.getByRole('status')).toHaveTextContent('正在加载项目')
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
    expect(await screen.findByRole('option', { name: 'Morning Brief' })).toHaveAttribute('aria-selected', 'true')

    rerender(
      <ProjectContextControl
        mode="select"
        projects={projects.map((project) => ({ ...project }))}
        value="article-1"
        onValueChange={vi.fn()}
      />,
    )

    expect(screen.getByPlaceholderText('搜索项目...')).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Morning Brief' })).toHaveAttribute('aria-selected', 'true')
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
