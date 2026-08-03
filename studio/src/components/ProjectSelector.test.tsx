import { fireEvent, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { render } from '@/test/test-utils'
import type { Project } from '@/types'
import { ProjectSelector } from './ProjectSelector'

vi.mock('@/lib/api', () => ({
  api: {
    projects: {
      list: vi.fn(),
    },
  },
}))

const projects: Project[] = [
  {
    id: 'article-1',
    user_id: 'user-1',
    platform: 'article',
    name: 'Morning Brief',
    avatar_url: 'https://example.com/morning.png',
    profile_url: 'https://example.com/morning',
    description: 'Daily editorial briefing',
    keywords: 'news',
    instructions: 'Write a concise morning brief',
    visual_style: 'editorial',
    writer: 'default',
    theme: 'default',
    author: 'Editorial Desk',

    image_ratio: '16:9',
    max_concurrent_tasks: 2,
    config: {},
    status: 'active',
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
  },
  {
    id: 'seednote-1',
    user_id: 'user-1',
    platform: 'seednote',
    name: 'Garden Notes',
    avatar_url: 'https://example.com/garden.png',
    profile_url: 'https://example.com/garden',
    description: 'Seasonal planting journal',
    keywords: 'garden',
    instructions: 'Share practical planting notes',
    visual_style: 'natural',
    writer: 'default',
    theme: 'default',
    author: 'Garden Desk',

    image_ratio: '3:4',
    max_concurrent_tasks: 1,
    config: {},
    status: 'active',
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
  },
]

describe('ProjectSelector', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.projects.list).mockResolvedValue(projects)
  })

  it('shows a compact selected trigger and complete project identities in the popup', async () => {
    render(<ProjectSelector value="article-1" onChange={vi.fn()} />)

    const trigger = await screen.findByRole('combobox', { name: '筛选项目' })
    await waitFor(() => expect(trigger).toHaveTextContent('Morning Brief'))
    expect(trigger).not.toHaveTextContent('Daily editorial briefing')
    expect(trigger.querySelector('img')).toHaveAttribute('src', 'https://example.com/morning.png')

    fireEvent.click(trigger)
    const option = await screen.findByRole('option', { name: /Morning Brief/ })
    expect(option).toHaveTextContent('Daily editorial briefing')
    expect(option).not.toHaveTextContent('公众号项目')
    expect(option.querySelector('img')).toHaveAttribute('src', 'https://example.com/morning.png')
  })

  it('forwards project identity and clears to empty values', async () => {
    const onChange = vi.fn()
    render(<ProjectSelector value="" onChange={onChange} />)

    const trigger = await screen.findByRole('combobox', { name: '筛选项目' })
    await waitFor(() => expect(trigger).toHaveTextContent('全部项目'))
    fireEvent.click(trigger)
    fireEvent.click(await screen.findByRole('option', { name: /Garden Notes/ }))
    expect(onChange).toHaveBeenCalledWith('seednote-1', 'seednote')

    fireEvent.click(trigger)
    fireEvent.click(await screen.findByRole('option', { name: '全部项目' }))
    expect(onChange).toHaveBeenLastCalledWith('', '')
  })

  it('keeps platform querying and excludes configured platforms from the popup', async () => {
    render(
      <ProjectSelector
        value=""
        onChange={vi.fn()}
        platform="article"
        excludePlatforms={['seednote']}
      />,
    )

    const trigger = await screen.findByRole('combobox', { name: '筛选项目' })
    await waitFor(() => {
      expect(api.projects.list).toHaveBeenCalledWith({ status: 'active', platform: 'article' })
    })
    fireEvent.click(trigger)
    expect(await screen.findByRole('option', { name: /Morning Brief/ })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: /Garden Notes/ })).not.toBeInTheDocument()
  })
})
